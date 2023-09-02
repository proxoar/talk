package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/proxoar/talk/pkg/ability"
	"github.com/proxoar/talk/pkg/client"

	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
)

const (
	systemRoleContent = "You are a helpful assistant!"
)

type ChatGPT struct {
	Client *openai.Client
	Logger *zap.Logger
}

func (c *ChatGPT) MustFunction(_ context.Context) {
	m := client.Message{
		Role:    "user",
		Content: "Hello!",
	}
	o := ability.DefaultChatGPTOption()
	content, err := c.Completion(context.Background(), []client.Message{m}, ability.LLMOption{ChatGPT: o})
	if err != nil {
		c.Logger.Sugar().Panic("failed to get response from ChatGPTAb server: ", err)
	}
	if len(content) == 0 {
		c.Logger.Warn(`bad smell: got empty content from ChatGPTAb server`)
	}
	c.Logger.Info("ChatGPTAb is healthy")
}

func (c *ChatGPT) Quota(_ context.Context) (used, total int, err error) {
	// openai.Client doesn't support billing query
	return 0, 0, nil
}

func (c *ChatGPT) Completion(ctx context.Context, ms []client.Message, t ability.LLMOption) (string, error) {
	c.Logger.Info("completion...")
	if t.ChatGPT == nil {
		return "", errors.New("client did not provide ChatGPTAb option")
	}

	messages := messageOfComplete(ms)

	req := openai.ChatCompletionRequest{
		Messages:         messages,
		Model:            t.ChatGPT.Model,
		MaxTokens:        t.ChatGPT.MaxTokens,
		Temperature:      t.ChatGPT.Temperature,
		PresencePenalty:  t.ChatGPT.PresencePenalty,
		FrequencyPenalty: t.ChatGPT.FrequencyPenalty,
	}

	resp, err := c.Client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("CreateChatCompletion %+v: %v", t, err)
	}

	c.Logger.Sugar().Debug("complete result", resp)
	content := resp.Choices[0].Message.Content
	c.Logger.Sugar().Info("content:", content)
	return content, nil
}

// CompletionStream
//
// Return only one chunk that contains the whole content if stream is not supported.
// To make sure the chan closes eventually, caller should either read the last chunk from chan
// or got a chunk whose Err != nil
func (c *ChatGPT) CompletionStream(ctx context.Context, ms []client.Message, t ability.LLMOption) <-chan client.Chunk {
	c.Logger.Info("completion stream...")
	ch := make(chan client.Chunk, 64)
	if t.ChatGPT == nil {
		ch <- client.Chunk{Message: "", Err: errors.New("client did not provide ChatGPTAb option")}
		return ch
	}

	messages := messageOfComplete(ms)

	req := openai.ChatCompletionRequest{
		Messages:         messages,
		Model:            t.ChatGPT.Model,
		MaxTokens:        t.ChatGPT.MaxTokens,
		Temperature:      t.ChatGPT.Temperature,
		PresencePenalty:  t.ChatGPT.PresencePenalty,
		FrequencyPenalty: t.ChatGPT.FrequencyPenalty,
	}

	go func() {
		stream, err := c.Client.CreateChatCompletionStream(ctx, req)
		if err != nil {
			ch <- client.Chunk{Message: "", Err: err}
			return
		}
		defer stream.Close()
		defer close(ch)

		for {
			response, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}

			if err != nil {
				ch <- client.Chunk{Message: "", Err: err}
				break
			}
			ch <- client.Chunk{Message: response.Choices[0].Delta.Content, Err: nil}
		}
	}()
	return ch
}

// SetAbility set `ChatGPTAb` and `available` field of ability.LLMAb
func (c *ChatGPT) SetAbility(ctx context.Context, a *ability.LLMAb) error {
	models, err := c.GetModels(ctx)
	if err != nil {
		return err
	}
	a.Available = true
	a.ChatGPT = ability.ChatGPTAb{
		Available: true,
		Models:    models,
	}
	return nil
}

func (c *ChatGPT) GetModels(ctx context.Context) ([]string, error) {
	c.Logger.Info("completion...")
	ml, err := c.Client.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(ml.Models))
	for i := 0; i < len(ml.Models); i++ {
		if strings.Contains(ml.Models[i].ID, "gpt") {
			models = append(models, ml.Models[i].ID)
		}
	}
	sort.Strings(models)
	return models, err
}

func messageOfComplete(ms []client.Message) []openai.ChatCompletionMessage {
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemRoleContent,
		},
	}
	for _, m := range ms {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return messages
}