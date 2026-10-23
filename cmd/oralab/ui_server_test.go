package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeManagerOperations struct {
	stateValue      managerState
	lifecycleResult operationResult
	lifecycleCalls  int
	logValue        string
}

func (m *fakeManagerOperations) state(context.Context) managerState {
	return m.stateValue
}

func (m *fakeManagerOperations) logs(context.Context) (string, error) {
	return m.logValue, nil
}

func (m *fakeManagerOperations) lifecycle(_ context.Context, _ lifecycleRequest) operationResult {
	m.lifecycleCalls++
	return m.lifecycleResult
}

func testUIHandler(t *testing.T, manager managerOperations) *uiHandler {
	t.Helper()
	staticFS, err := fsSubForTest()
	if err != nil {
		t.Fatal(err)
	}
	return &uiHandler{manager: manager, csrf: "test-token", port: 8765, static: http.FileServer(http.FS(staticFS))}
}

func fsSubForTest() (fs.FS, error) {
	sub, err := fs.Sub(webAssets, "web")
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func TestUIIndexIsLocalAndSecurityHardened(t *testing.T) {
	handler := testUIHandler(t, &fakeManagerOperations{})
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/", nil)
	request.Host = "127.0.0.1:8765"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `name="oralab-csrf" content="test-token"`) {
		t.Fatal("index does not contain the session CSRF token")
	}
	if recorder.Header().Get("Content-Security-Policy") == "" || recorder.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers are missing")
	}
	if recorder.Header().Get("X-OraLab-Manager") != "2" {
		t.Fatal("manager identity header is missing")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %#v", cookies)
	}
}

func TestUIRejectsUntrustedHost(t *testing.T) {
	handler := testUIHandler(t, &fakeManagerOperations{})
	request := httptest.NewRequest(http.MethodGet, "http://evil.example/", nil)
	request.Host = "evil.example"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestMutationRequiresOriginCookieAndCSRFHeader(t *testing.T) {
	manager := &fakeManagerOperations{lifecycleResult: operationResult{OK: true}}
	handler := testUIHandler(t, manager)
	body := `{"action":"stop"}`

	unauthorized := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/api/lifecycle", strings.NewReader(body))
	unauthorized.Host = "127.0.0.1:8765"
	unauthorized.Header.Set("Content-Type", "application/json")
	unauthorized.Header.Set("Origin", "http://127.0.0.1:8765")
	unauthorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusForbidden || manager.lifecycleCalls != 0 {
		t.Fatalf("unauthorized mutation status=%d calls=%d", unauthorizedRecorder.Code, manager.lifecycleCalls)
	}

	authorized := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/api/lifecycle", strings.NewReader(body))
	authorized.Host = "127.0.0.1:8765"
	authorized.Header.Set("Content-Type", "application/json")
	authorized.Header.Set("Origin", "http://127.0.0.1:8765")
	authorized.Header.Set("X-OraLab-CSRF", "test-token")
	authorized.AddCookie(&http.Cookie{Name: "oralab_session", Value: "test-token"})
	authorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRecorder, authorized)
	if authorizedRecorder.Code != http.StatusOK || manager.lifecycleCalls != 1 {
		t.Fatalf("authorized mutation status=%d calls=%d body=%s", authorizedRecorder.Code, manager.lifecycleCalls, authorizedRecorder.Body.String())
	}
}

func TestBrowserCannotExecuteArbitraryCommands(t *testing.T) {
	handler := testUIHandler(t, &fakeManagerOperations{})
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/api/exec", strings.NewReader(`{"command":"rm"}`))
	request.Host = "127.0.0.1:8765"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestMutationRejectsTrailingJSON(t *testing.T) {
	manager := &fakeManagerOperations{lifecycleResult: operationResult{OK: true}}
	handler := testUIHandler(t, manager)
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/api/lifecycle", strings.NewReader(`{"action":"stop"} {}`))
	request.Host = "127.0.0.1:8765"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8765")
	request.Header.Set("X-OraLab-CSRF", "test-token")
	request.AddCookie(&http.Cookie{Name: "oralab_session", Value: "test-token"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || manager.lifecycleCalls != 0 {
		t.Fatalf("status=%d calls=%d", recorder.Code, manager.lifecycleCalls)
	}
}

func TestUILogsAreReadOnlyAndBoundedByManager(t *testing.T) {
	handler := testUIHandler(t, &fakeManagerOperations{logValue: "latest lines"})
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/logs", nil)
	request.Host = "127.0.0.1:8765"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "latest lines") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
