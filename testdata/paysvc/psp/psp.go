package psp

import (
	"context"
	"net/http"
	"strings"
)

type Gateway interface {
	Authorize(ctx context.Context, ref string, cents int64) (string, error)
}

type HTTPGateway struct{ BaseURL string }

func (g *HTTPGateway) Authorize(ctx context.Context, ref string, cents int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.BaseURL+"/authorize",
		strings.NewReader(ref))
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return resp.Header.Get("X-Auth-Ref"), nil
}
