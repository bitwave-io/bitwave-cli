package addresssvc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupSymbolAndSpamThreshold(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/symbols/TUSD" || r.Header.Get("Accept") != "application/json" {
			t.Fatalf("request = %s accept=%s", r.URL.Path, r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(`{"coinId":1031,"networkId":"eth","address":"0x0000000000085d4780b73119b644ae5ecd22b376","symbol":"TUSD","spamScore":0.5}`))
	}))
	defer server.Close()

	coin, err := New(server.URL).LookupSymbol(context.Background(), "TUSD")
	if err != nil {
		t.Fatal(err)
	}
	if coin.CoinID != 1031 || coin.Address != "0x0000000000085d4780b73119b644ae5ecd22b376" {
		t.Fatalf("coin = %#v", coin)
	}
	if !IsSpam(coin, DefaultSpamThreshold) {
		t.Fatalf("score %#v should meet the threshold", coin.SpamScore)
	}
}
