package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	"github.com/sashabaranov/go-openai"
)

// Streamer реализуется клиентом на этапе сборки приложения: чату брифа нужен
// именно стриминговый вызов, и утверждение типа должно падать при сборке, а не
// в рантайме.
var _ corellm.Streamer = (*OpenAIClient)(nil)

// CompleteStream выполняет вызов модели в потоковом режиме: каждый непустой
// фрагмент текста уходит в onDelta, а возвращаемый Usage содержит токены,
// накопленный ответ и одну запись расхода.
//
// Отличия от Complete здесь намеренные: JSON-режим не запрашивается (нужна
// проза), ретраев нет (частично отданный поток нельзя переиграть) — ошибка
// уходит наверх, пользователь повторит ход.
func (c *OpenAIClient) CompleteStream(ctx context.Context, role, system, user string, onDelta func(string)) (corellm.Usage, error) {
	req := openai.ChatCompletionRequest{
		Model: c.ModelFor(role),
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: system},
			{Role: openai.ChatMessageRoleUser, Content: user},
		},
		// IncludeUsage заставляет провайдера прислать расход отдельным чанком
		// перед [DONE]: в стриме токенов в теле ответа больше нет.
		StreamOptions: &openai.StreamOptions{IncludeUsage: true},
	}

	stream, err := c.api.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return corellm.Usage{}, err
	}
	defer stream.Close()

	var (
		text  strings.Builder
		usage corellm.Usage
		model string
	)
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Накопленный текст не теряем: вызывающая сторона решает, что с ним
			// делать — реплика уже могла дойти до пользователя.
			return finishStream(usage, text.String(), model, req.Model, role), fmt.Errorf("llm: поток прерван: %w", err)
		}
		if model == "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			usage.PromptTokens = chunk.Usage.PromptTokens
			usage.CompletionTokens = chunk.Usage.CompletionTokens
			if d := chunk.Usage.CompletionTokensDetails; d != nil {
				usage.ReasoningTokens = d.ReasoningTokens
			}
		}
		for _, choice := range chunk.Choices {
			if delta := choice.Delta.Content; delta != "" {
				text.WriteString(delta)
				onDelta(delta)
			}
			// Размышления reasoning-моделей в стриме приходят отдельной дельтой:
			// наружу их не отдаём, но сохраняем для трассы.
			usage.Reasoning += choice.Delta.ReasoningContent
			if choice.FinishReason != "" {
				usage.FinishReason = string(choice.FinishReason)
			}
		}
	}

	if text.Len() == 0 {
		// Провайдер мог выставить счёт и за пустой ответ: расход возвращаем
		// вместе с ошибкой, как это делает Complete.
		return finishStream(usage, "", model, req.Model, role), errors.New("llm: пустой поток ответа")
	}
	return finishStream(usage, text.String(), model, req.Model, role), nil
}

// finishStream дописывает в usage то, что известно только по завершении потока:
// накопленный ответ, фактическую модель и запись расхода для учёта стоимости.
func finishStream(usage corellm.Usage, response, model, requestedModel, role string) corellm.Usage {
	if model == "" {
		model = requestedModel
	}
	usage.Response = response
	usage.Entries = []corellm.UsageEntry{{
		Model:            model,
		Role:             role,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
	}}
	return usage
}
