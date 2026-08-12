//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 30, G: 120, B: 240, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

func TestRunImageCheckForModelGeneratesAndDecodesOneImage(t *testing.T) {
	swapMonitorHTTPClient(t)
	imageBytes := testPNGBytes(t)
	var requestCount int
	var requestBody map[string]any
	var requestPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		requestPath = r.URL.Path
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{
				"b64_json": base64.StdEncoding.EncodeToString(imageBytes),
			}},
		})
	}))
	t.Cleanup(server.Close)

	result, payload := runImageCheckForModel(
		context.Background(),
		server.URL,
		"sk-test",
		"gpt-image-2",
		&CheckOptions{
			APIMode: MonitorAPIModeImages,
			ExtraHeaders: map[string]string{
				"X-Monitor-Test": "true",
			},
		},
	)

	if result.Status != MonitorStatusOperational {
		t.Fatalf("expected operational image check, got %s: %s", result.Status, result.Message)
	}
	if payload == nil {
		t.Fatal("successful image check should return a cache payload")
	}
	if payload.ContentType != "image/png" {
		t.Fatalf("expected PNG content type, got %q", payload.ContentType)
	}
	if !bytes.Equal(payload.Data, imageBytes) {
		t.Fatal("decoded image bytes differ from upstream image")
	}
	if requestCount != 1 {
		t.Fatalf("expected one real image request, got %d", requestCount)
	}
	if requestPath != providerOpenAIImagesPath {
		t.Fatalf("expected image path %q, got %q", providerOpenAIImagesPath, requestPath)
	}
	if requestBody["model"] != "gpt-image-2" {
		t.Fatalf("expected configured model, got %#v", requestBody["model"])
	}
	if requestBody["n"] != float64(1) || requestBody["response_format"] != "b64_json" {
		t.Fatalf("unexpected image request body: %#v", requestBody)
	}
}

func TestRunImageCheckForModelUsesDedicatedImageClient(t *testing.T) {
	imageBytes := testPNGBytes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{
				"b64_json": base64.StdEncoding.EncodeToString(imageBytes),
			}},
		})
	}))
	t.Cleanup(server.Close)

	origTextClient := monitorHTTPClient
	origImageClient := monitorImageHTTPClient
	monitorHTTPClient = &http.Client{Timeout: 5 * time.Millisecond}
	monitorImageHTTPClient = &http.Client{Timeout: 250 * time.Millisecond}
	t.Cleanup(func() {
		monitorHTTPClient = origTextClient
		monitorImageHTTPClient = origImageClient
	})

	result, payload := runImageCheckForModel(
		context.Background(),
		server.URL,
		"sk-test",
		"gpt-image-2",
		&CheckOptions{APIMode: MonitorAPIModeImages},
	)

	if result.Status != MonitorStatusOperational {
		t.Fatalf("expected image client to outlive text timeout, got %s: %s", result.Status, result.Message)
	}
	if payload == nil {
		t.Fatal("successful image check should return a cache payload")
	}
}

func TestRunImageCheckForModelUsesConfiguredWaitLimit(t *testing.T) {
	imageBytes := testPNGBytes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{
				"b64_json": base64.StdEncoding.EncodeToString(imageBytes),
			}},
		})
	}))
	t.Cleanup(server.Close)

	originalImageClient := monitorImageHTTPClient
	monitorImageHTTPClient = &http.Client{Timeout: time.Second}
	t.Cleanup(func() {
		monitorImageHTTPClient = originalImageClient
	})

	start := time.Now()
	result, payload := runImageCheckForModel(
		context.Background(),
		server.URL,
		"sk-test",
		"gpt-image-2",
		&CheckOptions{
			APIMode:        MonitorAPIModeImages,
			RequestTimeout: 20 * time.Millisecond,
		},
	)

	if result.Status != MonitorStatusError {
		t.Fatalf("expected configured timeout to fail the image check, got %s: %s", result.Status, result.Message)
	}
	if payload != nil {
		t.Fatal("timed out image check must not produce a cache payload")
	}
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Fatalf("configured image wait limit was ignored: elapsed=%s", elapsed)
	}
}

func TestRunChecksConcurrentImagesOnlyChecksPrimaryModel(t *testing.T) {
	swapMonitorHTTPClient(t)
	imageBytes := testPNGBytes(t)
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{
				"b64_json": base64.StdEncoding.EncodeToString(imageBytes),
			}},
		})
	}))
	t.Cleanup(server.Close)

	svc := NewChannelMonitorService(nil, nil)
	results, payload := svc.runChecksConcurrent(context.Background(), &ChannelMonitor{
		Provider:     MonitorProviderOpenAI,
		APIMode:      MonitorAPIModeImages,
		Endpoint:     server.URL,
		APIKey:       "sk-test",
		PrimaryModel: "gpt-image-2",
		ExtraModels:  []string{"gpt-image-2-extra"},
	})

	if len(results) != 1 || results[0].Model != "gpt-image-2" {
		t.Fatalf("images mode should return only primary result, got %#v", results)
	}
	if payload == nil {
		t.Fatal("images mode should return a successful cache payload")
	}
	if requestCount != 1 {
		t.Fatalf("images mode should generate once even with extra models, got %d requests", requestCount)
	}
}

func TestRunImageCheckForModelFailureDoesNotReturnCachePayload(t *testing.T) {
	swapMonitorHTTPClient(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"image unavailable"}}`, http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	result, payload := runImageCheckForModel(
		context.Background(),
		server.URL,
		"sk-test",
		"gpt-image-2",
		&CheckOptions{APIMode: MonitorAPIModeImages},
	)

	if result.Status != MonitorStatusError {
		t.Fatalf("expected error status, got %s", result.Status)
	}
	if payload != nil {
		t.Fatal("failed image check must not replace the cached image")
	}
}

func TestValidateAPIModeImagesOnlySupportsOpenAI(t *testing.T) {
	if err := validateAPIMode(MonitorProviderOpenAI, MonitorAPIModeImages); err != nil {
		t.Fatalf("OpenAI images mode should be valid: %v", err)
	}
	if err := validateAPIMode(MonitorProviderGrok, MonitorAPIModeImages); err == nil {
		t.Fatal("non-OpenAI images mode should be rejected")
	}
}
