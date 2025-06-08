package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	w "github.com/suryanshu-09/we-go-wayback/waybackdiscoverdiff"
)

func TestSimhashParams(t *testing.T) {
	t.Run("test Simhash Params /simhash?timestamp=20141115130953", func(t *testing.T) {
		StubRedis()
		clientMock.MatchExpectationsInOrder(false)
		handle := http.HandlerFunc(w.ServeSimhash(redisClient))
		srv := httptest.NewServer(handle)
		defer srv.Close()

		req := httptest.NewRequest("GET", "/simhash?timestamp=20141115130953", nil)

		resp := httptest.NewRecorder()

		handle.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Errorf("got status:%v\nwant status:%v", resp.Code, http.StatusOK)
		}

		want, _ := json.Marshal(w.HttpResponse{Status: "error", Info: "url param is required."})
		got := resp.Body.String()

		if reflect.DeepEqual(got, want) {
			t.Errorf("got:%s\nwant:%s", got, want)
		}
		clientMock.ExpectationsWereMet()
	})
	t.Run("test Simhash Params /simhash?url=example.com", func(t *testing.T) {
		StubRedis()
		clientMock.MatchExpectationsInOrder(false)
		handle := http.HandlerFunc(w.ServeSimhash(redisClient))
		srv := httptest.NewServer(handle)
		defer srv.Close()

		req := httptest.NewRequest("GET", "/simhash?url=example.com", nil)

		resp := httptest.NewRecorder()

		handle.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Errorf("got status:%v\nwant status:%v", resp.Code, http.StatusOK)
		}

		want, _ := json.Marshal(w.HttpResponse{Status: "error", Info: "year param is required."})
		got := resp.Body.String()

		if reflect.DeepEqual(got, want) {
			t.Errorf("got:%s\nwant:%s", got, want)
		}
		clientMock.ExpectationsWereMet()
	})
	t.Run("test Simhash Params /simhash?url=invalid&timestamp=20141115130953", func(t *testing.T) {
		StubRedis()
		clientMock.MatchExpectationsInOrder(false)
		handle := http.HandlerFunc(w.ServeSimhash(redisClient))
		srv := httptest.NewServer(handle)
		defer srv.Close()

		req := httptest.NewRequest("GET", "/simhash?url=invalid&timestamp=20141115130953", nil)

		resp := httptest.NewRecorder()

		handle.ServeHTTP(resp, req)
		fmt.Println(resp.Code)

		if resp.Code != http.StatusOK {
			t.Errorf("got status:%v\nwant status:%v", resp.Code, http.StatusOK)
		}

		want, _ := json.Marshal(w.HttpResponse{Status: "error", Info: "invalid url format."})
		got := resp.Body.String()

		if reflect.DeepEqual(got, want) {
			t.Errorf("got:%s\nwant:%s", got, want)
		}
		clientMock.ExpectationsWereMet()
	})
	t.Run("test Simhash Params /simhash?url=example.com&timestamp=20140202131837", func(t *testing.T) {
		StubRedis()
		clientMock.MatchExpectationsInOrder(false)
		handle := http.HandlerFunc(w.ServeSimhash(redisClient))
		srv := httptest.NewServer(handle)
		defer srv.Close()

		req := httptest.NewRequest("GET", "/simhash?url=example.com&timestamp=20140202131837", nil)

		resp := httptest.NewRecorder()

		handle.ServeHTTP(resp, req)

		want, _ := json.Marshal(w.HttpResponse{Status: "success", Simhash: "og2jGKWHsy4="})
		got := resp.Body.String()

		if reflect.DeepEqual(got, want) {
			t.Errorf("got:%s\nwant:%s", got, want)
		}
		clientMock.ExpectationsWereMet()
	})
}

func TestNoEntry(t *testing.T) {
	StubRedis()
	clientMock.MatchExpectationsInOrder(false)

	handle := http.HandlerFunc(w.ServeSimhash(redisClient))
	srv := httptest.NewServer(handle)
	defer srv.Close()

	req := httptest.NewRequest("GET", "/simhash?timestamp=20180000000000&url=nonexistingdomain.org", nil)

	resp := httptest.NewRecorder()

	handle.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("got status:%v\nwant status:%v", resp.Code, http.StatusOK)
	}
	want, _ := json.Marshal(w.HttpResponse{Message: "CAPTURE_NOT_FOUND", Status: "error"})
	got := resp.Body.String()

	if reflect.DeepEqual(got, want) {
		t.Errorf("got:%s\nwant:%s", got, want)
	}
	clientMock.ExpectationsWereMet()
}

func TestNoSnapshots(t *testing.T) {
	StubRedis()
	clientMock.MatchExpectationsInOrder(false)

	handle := http.HandlerFunc(w.ServeSimhash(redisClient))
	srv := httptest.NewServer(handle)
	defer srv.Close()

	req := httptest.NewRequest("GET", "/simhash?url=nonexistingdomain.org&year=1999", nil)

	resp := httptest.NewRecorder()

	handle.ServeHTTP(resp, req)

	want, _ := json.Marshal(w.HttpResponse{Message: "NO_CAPTURES", Status: "error"})
	got := resp.Body.String()

	if reflect.DeepEqual(got, want) {
		t.Errorf("got:%s\nwant:%s", got, want)
	}
	clientMock.ExpectationsWereMet()
}

func TestStartTask(t *testing.T) {
	handle := http.HandlerFunc(w.ServeRoot)
	srv := httptest.NewServer(handle)
	defer srv.Close()

	req := httptest.NewRequest("GET", "/", nil)

	resp := httptest.NewRecorder()

	handle.ServeHTTP(resp, req)

	if resp.Body.String() == "" {
		t.Errorf("got:%s:", resp.Body.String())
	}
}

func TestSimhashParamValidation(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantStatus string
		wantInfo   string
	}{
		{
			name:       "missing url",
			query:      "/calculate-simhash?year=2018",
			wantStatus: "error",
			wantInfo:   "url param is required.",
		},
		{
			name:       "invalid year XY",
			query:      "/calculate-simhash?url=example.com&year=XY",
			wantStatus: "error",
			wantInfo:   "year param is required.",
		},
		{
			name:       "missing year for existing url",
			query:      "/calculate-simhash?url=nonexistingdomain.org",
			wantStatus: "error",
			wantInfo:   "year param is required.",
		},
		{
			name:       "year is dash",
			query:      "/calculate-simhash?url=nonexistingdomain.org&year=-",
			wantStatus: "error",
			wantInfo:   "year param is required.",
		},
		{
			name:       "invalid url format",
			query:      "/calculate-simhash?url=foo&year=2000",
			wantStatus: "error",
			wantInfo:   "invalid url format.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			StubRedis()
			clientMock.MatchExpectationsInOrder(false)
			handler := http.HandlerFunc(w.ServeCalculateSimhash(redisClient))
			req := httptest.NewRequest("GET", tc.query, nil)
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK {
				t.Errorf("got status code %d, want %d", resp.Code, http.StatusOK)
			}

			// Parse both responses as JSON objects for comparison
			var got, want w.HttpResponse
			if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			want = w.HttpResponse{Status: tc.wantStatus, Info: tc.wantInfo}

			if got.Status != want.Status || got.Info != want.Info {
				t.Errorf("response mismatch:\ngot:  {Status: %q, Info: %q}\nwant: {Status: %q, Info: %q}",
					got.Status, got.Info, want.Status, want.Info)
			}

			clientMock.ExpectationsWereMet()
		})
	}
}

func TestJobParams(t *testing.T) {
	StubRedis()
	clientMock.MatchExpectationsInOrder(false)
	clientMock.ExpectationsWereMet()

	handler := http.HandlerFunc(w.ServeJob(redisClient))
	req := httptest.NewRequest("GET", "/job", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	// Parse both responses as JSON objects for comparison
	var got, want w.HttpResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	want = w.HttpResponse{Status: "error", Info: "job_id param is required."}

	if got.Status != want.Status || got.Info != want.Info {
		t.Errorf("response mismatch:\ngot:  {Status: %q, Info: %q}\nwant: {Status: %q, Info: %q}",
			got.Status, got.Info, want.Status, want.Info)
	}
}
