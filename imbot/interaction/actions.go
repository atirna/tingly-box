package interaction

import (
	"time"

	"github.com/tingly-dev/tingly-box/imbot/core"
)

// Building buttons for an interaction request used to be a per-platform job:
// each adapter implemented BuildMarkup and produced its own payload type,
// because SendMessageOptions had nowhere neutral to put controls. Five
// implementations of the same idea drifted apart — Feishu's wrote a button
// value of {"action": value}, dropping both the namespace and the interaction
// ID that its own ParseResponse then looked for, so no Feishu interaction
// could ever have resolved. That it went unnoticed is explained by there
// being no inbound card path on Feishu at all until recently.
//
// With core.ActionSet and core.Payload there is nothing platform-specific
// left in the job: an interaction is a label and some segments to send back.

// actionNamespace prefixes every payload this package produces, so an
// interaction callback is distinguishable from the application's own buttons
// sharing the same platform.
const actionNamespace = "ia"

// BuildActions renders interaction options as neutral actions. Input actions
// have no button form and are skipped; navigation actions share a row, which
// is what makes them read as a strip rather than a stack.
func BuildActions(interactions []Interaction) *core.ActionSet {
	set := core.NewActionSet()
	var navRow []core.Action

	flushNav := func() {
		if len(navRow) > 0 {
			set.AddRow(navRow...)
			navRow = nil
		}
	}

	for _, item := range interactions {
		action := core.Action{
			ID:      item.ID,
			Label:   item.Label,
			Payload: core.NewPayload(actionNamespace, item.ID, item.Value),
		}
		switch item.Type {
		case ActionSelect, ActionConfirm, ActionCancel:
			flushNav()
			set.AddRow(action)
		case ActionNavigate:
			navRow = append(navRow, action)
		case ActionInput:
			continue
		}
	}
	flushNav()
	return set
}

// ParseActionResponse reads an interaction reply out of an inbound message.
//
// It returns ErrNotInteraction for a button press belonging to someone else's
// namespace, and (nil, nil) for a message that is not a button press at all —
// the caller then tries to read it as a numbered text reply.
func ParseActionResponse(msg core.Message) (*InteractionResponse, error) {
	if !msg.IsCallback() {
		return nil, nil
	}
	payload := msg.Payload
	if payload.IsEmpty() {
		payload = core.PayloadFromCallbackData(callbackDataOf(msg))
	}
	if payload.Name() != actionNamespace || len(payload) < 3 {
		return nil, ErrNotInteraction
	}

	timestamp := time.Unix(msg.Timestamp, 0)
	// Four segments carry an explicit request ID: ia, interactionID, requestID,
	// value. Three carry only the interaction: ia, interactionID, value.
	if len(payload) >= 4 {
		return &InteractionResponse{
			RequestID: payload.Arg(2),
			Action:    Interaction{ID: payload.Arg(1), Value: payload.Arg(3)},
			Timestamp: timestamp,
		}, nil
	}
	return &InteractionResponse{
		Action:    Interaction{ID: payload.Arg(1), Value: payload.Arg(2)},
		Timestamp: timestamp,
	}, nil
}

// callbackDataOf reads the legacy flat form for platforms whose inbound
// adapters have not been taught to fill Message.Payload yet.
func callbackDataOf(msg core.Message) string {
	data, _ := msg.Metadata["callback_data"].(string)
	return data
}
