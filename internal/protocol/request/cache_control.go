package request

import (
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

// applyFirstResponsesCacheBreakpoint carries an Anthropic cache boundary that
// Responses cannot attach directly (for example, on a tool definition or tool
// call). The boundary advances to the first cacheable content block.
func applyFirstResponsesCacheBreakpoint(req *responses.ResponseNewParams) {
	if req.Instructions.Valid() && req.Instructions.Value != "" {
		text := &responses.ResponseInputTextParam{
			Text:                  req.Instructions.Value,
			PromptCacheBreakpoint: responses.NewResponseInputTextPromptCacheBreakpointParam(),
		}
		req.Instructions = param.Opt[string]{}
		system := responseMessageWithContent("system", responses.ResponseInputMessageContentListParam{
			{OfInputText: text},
		})
		req.Input.OfInputItemList = append(responses.ResponseInputParam{system}, req.Input.OfInputItemList...)
		return
	}

	for i := range req.Input.OfInputItemList {
		item := &req.Input.OfInputItemList[i]
		if item.OfMessage != nil {
			content := &item.OfMessage.Content
			if content.OfString.Valid() && content.OfString.Value != "" {
				text := &responses.ResponseInputTextParam{
					Text:                  content.OfString.Value,
					PromptCacheBreakpoint: responses.NewResponseInputTextPromptCacheBreakpointParam(),
				}
				content.OfString = param.Opt[string]{}
				content.OfInputItemContentList = responses.ResponseInputMessageContentListParam{
					{OfInputText: text},
				}
				return
			}
			for j := range content.OfInputItemContentList {
				part := &content.OfInputItemContentList[j]
				if part.OfInputText != nil {
					part.OfInputText.PromptCacheBreakpoint = responses.NewResponseInputTextPromptCacheBreakpointParam()
					return
				}
				if part.OfInputImage != nil {
					part.OfInputImage.PromptCacheBreakpoint = responses.NewResponseInputImagePromptCacheBreakpointParam()
					return
				}
			}
		}
	}
}
