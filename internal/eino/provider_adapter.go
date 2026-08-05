package eino

import (
	"context"
	"encoding/json"
	"io"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/simon/mneme/internal/inference"
)

// einoProvider adapts an eino model.BaseChatModel to the inference.Provider
// interface. This allows all existing auxiliary LLM consumers (desktop, learning,
// memory archivist, subconscious, JSON-RPC server) to migrate transparently.
type einoProvider struct {
	name  string
	model model.BaseChatModel
}

// NewEinoProvider wraps an eino chat model as an inference.Provider.
func NewEinoProvider(name string, m model.BaseChatModel) inference.Provider {
	return &einoProvider{name: name, model: m}
}

func (p *einoProvider) Name() string { return p.name }

func (p *einoProvider) Chat(ctx context.Context, req inference.ChatRequest) (<-chan inference.Token, <-chan error) {
	tokens := make(chan inference.Token, 64)
	errs := make(chan error, 1)

	go func() {
		defer close(tokens)
		defer close(errs)

		msgs := p.convertMessages(req)

		stream, err := p.model.Stream(ctx, msgs)
		if err != nil {
			errs <- err
			return
		}
		defer stream.Close()

		var fullText string
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				errs <- err
				return
			}
			if msg == nil {
				continue
			}

			// Emit text content as tokens.
			if msg.Content != "" {
				fullText += msg.Content
				tokens <- inference.Token{Text: msg.Content}
			}

			// Emit tool calls when present.
			for _, tc := range msg.ToolCalls {
				argsJSON := tc.Function.Arguments
				tokens <- inference.Token{
					ToolCall: &inference.ToolCall{
						ID:        tc.ID,
						Name:      tc.Function.Name,
						Arguments: json.RawMessage(argsJSON),
					},
				}
			}

			// Emit usage on final message.
			if msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
				tokens <- inference.Token{
					IsFinal: false, // the next IsFinal token is the actual final
					Usage: &inference.Usage{
						InputTokens:  msg.ResponseMeta.Usage.PromptTokens,
						OutputTokens: msg.ResponseMeta.Usage.CompletionTokens,
					},
				}
			}
		}

		tokens <- inference.Token{Text: fullText, IsFinal: true}
	}()

	return tokens, errs
}

// convertMessages maps inference.Message slice to eino *schema.Message slice.
func (p *einoProvider) convertMessages(req inference.ChatRequest) []*schema.Message {
	var msgs []*schema.Message

	// System prompt goes first as a system message.
	if req.SystemPrompt != "" {
		msgs = append(msgs, schema.SystemMessage(req.SystemPrompt))
	}

	for _, m := range req.Messages {
		var sMsg *schema.Message

		switch m.Role {
		case "user":
			if len(m.ContentBlocks) > 0 {
				// Multimodal user message.
				var parts []schema.ChatMessagePart
				for _, b := range m.ContentBlocks {
					switch b.Type {
					case "text":
						parts = append(parts, schema.ChatMessagePart{
							Type: schema.ChatMessagePartTypeText,
							Text: b.Text,
						})
					case "image":
						imageURL := b.ImageURL
						if imageURL == "" && b.ImageData != "" {
							imageURL = "data:" + b.ImageType + ";base64," + b.ImageData
						}
						parts = append(parts, schema.ChatMessagePart{
							Type:     schema.ChatMessagePartTypeImageURL,
							ImageURL: &schema.ChatMessageImageURL{URL: imageURL},
						})
					}
				}
				sMsg = &schema.Message{
					Role:         schema.User,
					MultiContent: parts,
				}
			} else {
				sMsg = schema.UserMessage(m.Content)
			}
		case "assistant":
			sMsg = schema.AssistantMessage(m.Content, nil)
			if m.ToolCall != nil {
				tc := schema.ToolCall{
					ID: m.ToolCall.ID,
					Function: schema.FunctionCall{
						Name:      m.ToolCall.Name,
						Arguments: string(m.ToolCall.Arguments),
					},
				}
				sMsg.ToolCalls = []schema.ToolCall{tc}
			}
		case "tool":
			sMsg = schema.ToolMessage(m.ToolID, m.Content)
		case "system":
			sMsg = schema.SystemMessage(m.Content)
		default:
			sMsg = schema.UserMessage(m.Content)
		}

		msgs = append(msgs, sMsg)
	}

	return msgs
}

// Ensure einoProvider satisfies inference.Provider at compile time.
var _ inference.Provider = (*einoProvider)(nil)
