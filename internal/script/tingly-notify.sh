#!/bin/bash
# Tingly Box Notify Hook for Claude Code (Push Only)
#
# This script handles PUSH-ONLY notifications that do NOT require user approval.
# It forwards the event to Tingly Box for desktop or IM delivery and exits immediately.
#
# Supported events:
#   - Stop (task completion notification)
#   - PostToolUse (tool finished notification)
#   - Notification with "completion" or other non-permission messages
#
# For INTERACTIVE approval hooks, use tingly-im-hook.sh instead.
#
# Usage (from Claude Code settings.json hooks):
#   {
#     "env": { "TINGLY_HOOK_TOKEN": "<your tingly-box user token>" },
#     "hooks": {
#       "Stop": [{
#         "matcher": "",
#         "hooks": [{ "type": "command", "command": "~/.claude/tingly-notify.sh" }]
#       }],
#       "Notification": [{
#         "matcher": "completion",
#         "hooks": [{ "type": "command", "command": "~/.claude/tingly-notify.sh" }]
#       }]
#     }
#   }
#
# TINGLY_HOOK_TOKEN is the same user token used to sign into the tingly-box
# web UI (Settings > user token) — /tingly/:scenario/notify requires it.
# Without it the server responds 401 and this script exits quietly.

set -u

CC_INPUT=$(cat)

TINGLY_API_URL="${TINGLY_API_URL:-http://localhost:12580}"
TINGLY_SCENARIO="${TINGLY_SCENARIO:-claude_code}"
TINGLY_HOOK_TOKEN="${TINGLY_HOOK_TOKEN:-}"

AUTH_HEADER=()
if [ -n "$TINGLY_HOOK_TOKEN" ]; then
  AUTH_HEADER=(-H "Authorization: Bearer ${TINGLY_HOOK_TOKEN}")
fi

# Forward the full Claude Code hook input to Tingly Box.
# This is fire-and-forget: we don't care about the response.
# The server will deliver desktop notification or forward to IM if configured.
echo "$CC_INPUT" | curl -s -X POST \
  -H "Content-Type: application/json" \
  "${AUTH_HEADER[@]}" \
  -d @- \
  "${TINGLY_API_URL}/tingly/${TINGLY_SCENARIO}/notify" 2>/dev/null || true

exit 0
