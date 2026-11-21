package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"blockchain.hanz.dev/manager/types"
)

func TestParseCTFDUser(t *testing.T) {
	user, err := parseCTFDUser([]byte(`{"success":true,"data":{"id":1,"name":"Player"}}`))
	if err != nil {
		t.Fatalf("parseCTFDUser returned error: %v", err)
	}
	if want := (types.CTFUser{Id: "1", Name: "Player"}); user != want {
		t.Errorf("user = %+v, want %+v", user, want)
	}
}

func TestParseCTFDUserRejectsFailure(t *testing.T) {
	cases := map[string]string{
		"unsuccessful": `{"success":false,"data":{"id":1,"name":"Player"}}`,
		"missing name": `{"success":true,"data":{"id":1}}`,
		"missing id":   `{"success":true,"data":{"name":"Player"}}`,
		"malformed":    `{`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCTFDUser([]byte(body)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseRCTFUser(t *testing.T) {
	user, err := parseRCTFUser([]byte(`{"kind":"goodUserSelfDataV2","message":"ok","data":{"id":"team-1a2b3c","name":"Player"}}`))
	if err != nil {
		t.Fatalf("parseRCTFUser returned error: %v", err)
	}
	if want := (types.CTFUser{Id: "team-1a2b3c", Name: "Player"}); user != want {
		t.Errorf("user = %+v, want %+v", user, want)
	}
}

func TestParseRCTFUserRejectsFailure(t *testing.T) {
	cases := map[string]string{
		"bad kind":     `{"kind":"badToken","message":"invalid token"}`,
		"missing name": `{"kind":"goodUserSelfDataV2","data":{"id":"team-1a2b3c"}}`,
		"missing id":   `{"kind":"goodUserSelfDataV2","data":{"name":"Player"}}`,
		"malformed":    `{`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRCTFUser([]byte(body)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestCTFDGetMeUsesTokenHeader(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{"success":true,"data":{"id":7,"name":"Player"}}`)
	}))
	defer server.Close()

	user := ctfdGetMe(server.URL+"/", "secret")
	if want := (types.CTFUser{Id: "7", Name: "Player"}); user != want {
		t.Errorf("user = %+v, want %+v", user, want)
	}
	if gotPath != "/api/v1/users/me" {
		t.Errorf("path = %q, want /api/v1/users/me", gotPath)
	}
	if gotAuth != "Token secret" {
		t.Errorf("authorization = %q, want Token secret", gotAuth)
	}
}

func TestRCTFGetMeUsesBearerHeader(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{"kind":"goodUserSelfDataV2","data":{"id":"team-1a2b3c","name":"Player"}}`)
	}))
	defer server.Close()

	user := rctfGetMe(server.URL+"/", "secret")
	if want := (types.CTFUser{Id: "team-1a2b3c", Name: "Player"}); user != want {
		t.Errorf("user = %+v, want %+v", user, want)
	}
	if gotPath != "/api/v2/users/me" {
		t.Errorf("path = %q, want /api/v2/users/me", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("authorization = %q, want Bearer secret", gotAuth)
	}
}

func TestLoadCTFConfig(t *testing.T) {
	t.Run("defaults to ctfd", func(t *testing.T) {
		t.Setenv("CTF_PLATFORM", "")
		t.Setenv("CTF_BASE", "")
		t.Setenv("CTFD_BASE", "https://ctfd.example.com/")
		t.Setenv("RCTF_BASE", "")

		config, err := loadCTFConfig()
		if err != nil {
			t.Fatalf("loadCTFConfig returned error: %v", err)
		}
		if config.platform != platformCTFd || config.baseURL != "https://ctfd.example.com" {
			t.Errorf("config = %+v", config)
		}
	})

	t.Run("rctf fallback base", func(t *testing.T) {
		t.Setenv("CTF_PLATFORM", "RCTF")
		t.Setenv("CTF_BASE", "")
		t.Setenv("CTFD_BASE", "https://ctfd.example.com")
		t.Setenv("RCTF_BASE", "https://rctf.example.com/")

		config, err := loadCTFConfig()
		if err != nil {
			t.Fatalf("loadCTFConfig returned error: %v", err)
		}
		if config.platform != platformRCTF || config.baseURL != "https://rctf.example.com" {
			t.Errorf("config = %+v", config)
		}
	})

	t.Run("CTF_BASE wins", func(t *testing.T) {
		t.Setenv("CTF_PLATFORM", "rctf")
		t.Setenv("CTF_BASE", "https://canonical.example.com")
		t.Setenv("RCTF_BASE", "https://fallback.example.com")

		config, err := loadCTFConfig()
		if err != nil {
			t.Fatalf("loadCTFConfig returned error: %v", err)
		}
		if config.baseURL != "https://canonical.example.com" {
			t.Errorf("baseURL = %q", config.baseURL)
		}
	})

	t.Run("rejects unknown platform", func(t *testing.T) {
		t.Setenv("CTF_PLATFORM", "ctfd2")
		t.Setenv("CTF_BASE", "https://example.com")

		if _, err := loadCTFConfig(); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("rejects missing base", func(t *testing.T) {
		t.Setenv("CTF_PLATFORM", "ctfd")
		t.Setenv("CTF_BASE", "")
		t.Setenv("CTFD_BASE", "")

		if _, err := loadCTFConfig(); err == nil {
			t.Fatal("expected an error")
		}
	})
}
