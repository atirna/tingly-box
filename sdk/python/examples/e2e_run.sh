#!/usr/bin/env bash
# End-to-end proof, using NO network and NO API keys:
#   client → tb (rule rag-demo) → Python provider → srv.use("experiment")
#          → tb (rule echo-model → vmodel provider) → echoed text → back to client
#
# The point of this script is as much what it does NOT do. There is no
# registration call, no plugin endpoint, no manifest. Step 4 creates the
# provider with the ordinary POST /api/v2/providers — byte for byte what the
# Connect AI dialog sends when you add Ollama — because that is the only path
# that exists.
#
# Prereqs:
#   go build -o /tmp/tb_e2e ./cli/tingly-box      # the tb binary
#   pip install httpx openai anthropic             # SDK deps
# Run:  bash sdk/python/examples/e2e_run.sh
set -uo pipefail

TB=${TB_BIN:-/tmp/tb_e2e}
CFG=$(mktemp -d)
PORT=18901
BASE="http://127.0.0.1:$PORT"
SRV_PORT=8765
SDK=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
export PYTHONPATH=$SDK
PY=${PYTHON_BIN:-python3}
FAILED=0

note() { echo "   $*"; }
check() { # check <label> <condition-result>
  if [[ "$2" == "0" ]]; then echo "   PASS  $1"; else echo "   FAIL  $1"; FAILED=1; fi
}

cleanup() {
  [[ -n "${SRV_PID:-}" ]] && kill "$SRV_PID" 2>/dev/null
  [[ -n "${TB_PID:-}" ]] && kill "$TB_PID" 2>/dev/null
}
trap cleanup EXIT

echo "== 1. start tb (config-dir=$CFG, port=$PORT) =="
"$TB" --config-dir "$CFG" start --port "$PORT" --ui --browser=false >/tmp/tb_e2e.log 2>&1 &
TB_PID=$!
for i in $(seq 1 60); do
  curl -sf "$BASE/api/v1/info/health" >/dev/null 2>&1 && break
  sleep 0.5
done
curl -sf "$BASE/api/v1/info/health" >/dev/null || { echo "tb did not start"; tail -25 /tmp/tb_e2e.log; exit 1; }
note "tb healthy at $BASE"

# Tokens are generated fresh per config-dir; read them from the config file.
CFGFILE=$(find "$CFG" -name 'config.json' | head -1)
UTOK=$($PY -c "import json;d=json.load(open('$CFGFILE'));print(d.get('user_token') or d.get('UserToken',''))")
MTOK=$($PY -c "import json;d=json.load(open('$CFGFILE'));print(d.get('model_token') or d.get('ModelToken',''))")
note "user token: ${UTOK:0:16}…   model token: ${MTOK:0:16}…"

UADMIN=(-H "Authorization: Bearer $UTOK" -H "Content-Type: application/json")
UMODEL=(-H "Authorization: Bearer $MTOK" -H "Content-Type: application/json")

echo "== 2. create the vmodel provider (in-process synthetic backend, no network) =="
note "AuthType=vmodel — a provider whose code is compiled into tb"
VUUID=$(curl -s "${UADMIN[@]}" -X POST "$BASE/api/v2/providers" -d '{
  "name":"vmodel-echo","api_base":"vmodel://local","api_style":"openai",
  "auth_type":"vmodel","no_key_required":true,"enabled":true}' \
  | $PY -c "import sys,json;d=json.load(sys.stdin);print(d.get('data',{}).get('uuid') or d.get('uuid',''))")
note "vmodel provider uuid: $VUUID"

echo "== 3. create the echo-model rule under the experiment scenario =="
note "this is the rule the Python provider will call BACK into"
curl -s "${UADMIN[@]}" -X POST "$BASE/api/v1/rule" -d "{
  \"scenario\":\"experiment\",\"request_model\":\"echo-model\",\"active\":true,
  \"lb_tactic\":{\"type\":\"random\",\"params\":{}},
  \"services\":[{\"provider\":\"$VUUID\",\"model\":\"echo-model\",\"weight\":1,\"active\":true}]}" \
  | $PY -c "import sys,json;d=json.load(sys.stdin);print('   rule created:', d.get('success'), d.get('data',{}).get('uuid',''))"

echo "== 4. start the Python server — it registers NOTHING =="
TINGLY_BOX_URL="$BASE" TINGLY_BOX_TOKEN="$UTOK" \
  $PY "$SDK/examples/e2e_server.py" >/tmp/py_server_e2e.log 2>&1 &
SRV_PID=$!
for i in $(seq 1 40); do
  curl -sf "http://127.0.0.1:$SRV_PORT/health" >/dev/null 2>&1 && break
  sleep 0.3
done
curl -sf "http://127.0.0.1:$SRV_PORT/health" >/dev/null || { echo "server did not start"; cat /tmp/py_server_e2e.log; exit 1; }
note "serving on http://127.0.0.1:$SRV_PORT (both /v1/messages and /v1/chat/completions)"

# tb's model-list refresh uses this; it is why the model id never has to be typed.
MODELS=$(curl -s "http://127.0.0.1:$SRV_PORT/v1/models")
echo "$MODELS" | grep -q '"rag-demo"'; check "GET /v1/models advertises rag-demo" $?

echo "== 5. add it to tb as an ORDINARY provider =="
note "POST /api/v2/providers — exactly what Connect AI → Self-hosted sends."
note "No plugin endpoint is involved because none exists."
PUUID=$(curl -s "${UADMIN[@]}" -X POST "$BASE/api/v2/providers" -d "{
  \"name\":\"rag-demo\",\"api_base\":\"http://127.0.0.1:$SRV_PORT\",
  \"api_style\":\"anthropic\",\"auth_type\":\"api_key\",
  \"no_key_required\":true,\"enabled\":true}" \
  | $PY -c "import sys,json;d=json.load(sys.stdin);print(d.get('data',{}).get('uuid') or d.get('uuid',''))")
note "python provider uuid: $PUUID"
[[ -n "$PUUID" ]]; check "provider created via the generic endpoint" $?

# supports_models_endpoint in the provider template means tb refreshes the
# model list off the server itself — the id is discovered, never typed.
curl -s "${UADMIN[@]}" -X POST "$BASE/api/v2/provider-models/$PUUID" -d '{}' | grep -q '"rag-demo"'
check "tb's model-list refresh discovered the model" $?

echo "== 6. bind a rule to it =="
curl -s "${UADMIN[@]}" -X POST "$BASE/api/v1/rule" -d "{
  \"scenario\":\"experiment\",\"request_model\":\"rag-demo\",\"active\":true,
  \"lb_tactic\":{\"type\":\"random\",\"params\":{}},
  \"services\":[{\"provider\":\"$PUUID\",\"model\":\"rag-demo\",\"weight\":1,\"active\":true}]}" \
  | $PY -c "import sys,json;d=json.load(sys.stdin);print('   rule created:', d.get('success'))"

echo "== 7. CLIENT CALL: OpenAI-shaped request for model=rag-demo =="
note "client speaks OpenAI → tb calls the Python provider as Anthropic"
note "→ handler calls back into tb → vmodel echoes → reshaped to chat.completion"
OUT=$(curl -s "${UMODEL[@]}" -X POST "$BASE/tingly/experiment/v1/chat/completions" -d '{
  "model":"rag-demo",
  "messages":[{"role":"user","content":"What is tingly-box?"}]}')
echo "$OUT" | $PY -m json.tool
echo "$OUT" | grep -q "tb echo returned"; check "round trip completed through the callback" $?

echo "== 8. no separate lifecycle: SIGKILL the server =="
note "the provider is a normal DB row, so it stays listed like any other"
kill -KILL "$SRV_PID" 2>/dev/null
SRV_PID=""
sleep 0.5
curl -s "${UADMIN[@]}" "$BASE/api/v2/providers" | grep -q 'rag-demo'
check "provider still listed after the process died" $?

echo "== 9. client call against the dead provider =="
note "liveness is the SAME per-service circuit breaker every provider gets."
note "No fallback tier is configured on this rule, so the request just errors —"
note "add a tier-1 real model and this would tier-failover instead."
curl -s "${UMODEL[@]}" -X POST "$BASE/tingly/experiment/v1/chat/completions" -d '{
  "model":"rag-demo",
  "messages":[{"role":"user","content":"still there?"}]}' \
  | $PY -c "import sys,json; d=json.load(sys.stdin); e=d.get('error', d); print('   ->', (json.dumps(e) if isinstance(e,dict) else str(e))[:200])"

echo "== 10. restart the server; the provider row is untouched =="
TINGLY_BOX_URL="$BASE" TINGLY_BOX_TOKEN="$UTOK" \
  $PY "$SDK/examples/e2e_server.py" >/tmp/py_server_e2e_2.log 2>&1 &
SRV_PID=$!
for i in $(seq 1 40); do
  curl -sf "http://127.0.0.1:$SRV_PORT/health" >/dev/null 2>&1 && break
  sleep 0.3
done
COUNT=$(curl -s "${UADMIN[@]}" "$BASE/api/v2/providers" \
  | $PY -c "import sys,json; d=json.load(sys.stdin); print(sum(1 for p in (d.get('data') or []) if p.get('name')=='rag-demo'))")
note "providers named rag-demo after restart: $COUNT (expect 1)"
[[ "$COUNT" == "1" ]]; check "no duplicate provider (nothing re-registers)" $?

OUT=$(curl -s "${UMODEL[@]}" -X POST "$BASE/tingly/experiment/v1/chat/completions" -d '{
  "model":"rag-demo",
  "messages":[{"role":"user","content":"What is tingly-box?"}]}')
echo "$OUT" | grep -q "tb echo returned"; check "traffic resumes with no reconfiguration" $?

echo
if [[ "$FAILED" == "0" ]]; then echo "== ALL CHECKS PASSED =="; else echo "== SOME CHECKS FAILED =="; fi
exit "$FAILED"
