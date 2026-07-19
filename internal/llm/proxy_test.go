package llm

import "testing"

func TestHTTPClientForInvalidProxy(t *testing.T) {
	_, err := httpClientFor("://bad")
	if err == nil {
		t.Fatal("expected invalid proxy error")
	}
}

func TestHTTPClientForEmptyUsesDefault(t *testing.T) {
	c, err := httpClientFor("")
	if err != nil {
		t.Fatal(err)
	}
	if c != defaultHTTPClient {
		t.Fatal("expected default client")
	}
}
