package router

import (
	"net/http"
	"testing"

	"proxy-go/internal/config"
	"proxy-go/internal/handler"
)

func TestSetupMainRoutesPrioritizesRemoteCacheEndpoint(t *testing.T) {
	t.Setenv("CACHE_CLEAR_REMOTE_TOKEN", "secret-token")
	proxyHandler := handler.NewProxyHandler(&config.Config{})
	mirrorHandler := handler.NewMirrorProxyHandler()

	t.Cleanup(func() {
		if proxyHandler.Cache != nil {
			proxyHandler.Cache.Stop()
		}
		if mirrorHandler.Cache != nil {
			mirrorHandler.Cache.Stop()
		}
	})

	routes := SetupMainRoutes(mirrorHandler, proxyHandler, nil)
	req, err := http.NewRequest(http.MethodPost, "/api/cache/clear-url", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	if !routes[0].Matcher(req) {
		t.Fatal("expected first main route to match /api/cache/clear-url")
	}
}
