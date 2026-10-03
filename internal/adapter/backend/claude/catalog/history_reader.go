package catalog

import (
	"context"
	"feidex/internal/application/backendops"
	"fmt"
	"strings"
)

type HistoryReader struct{}

func (HistoryReader) ReadConversationHistory(ctx context.Context, id string) (backendops.ThreadHistory, error) {
	if err := ctx.Err(); err != nil {
		return backendops.ThreadHistory{}, err
	}
	id = strings.TrimSpace(id)
	path, meta, err := FindSessionFile(id)
	if err != nil {
		return backendops.ThreadHistory{}, err
	}
	if strings.TrimSpace(path) == "" {
		return backendops.ThreadHistory{}, fmt.Errorf("未找到 Claude session `%s` 的本地 transcript", id)
	}
	turns, err := ReadHistoryTurns(path, false)
	if err != nil {
		return backendops.ThreadHistory{}, err
	}
	if err := ctx.Err(); err != nil {
		return backendops.ThreadHistory{}, err
	}
	result := backendops.ThreadHistory{ID: id}
	if meta != nil {
		result.Name, result.Preview, result.Cwd = meta.Title, meta.Preview, meta.Cwd
	}
	result.Turns = turns
	return result, nil
}
