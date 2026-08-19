package orgreports

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInvoicesUsesContactScopedPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		if r.URL.Path != "/invoices/org-1" || r.URL.Query().Get("contactId") != "ac-1.contact-1" || r.URL.Query().Get("lastRef") != "next-1" || r.URL.Query().Get("pageSize") != "25" {
			t.Fatalf("request URL = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"records":[{"id":"ac-1.invoice-1","title":"INV-1","status":"AwaitingPayment","dueAmount":12.34,"totalAmount":20,"type":"Receiving","contactId":"ac-1.contact-1","enabled":true}],"nextPageToken":"next-2"}`))
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	page, err := client.Invoices(context.Background(), "org-1", InvoiceListInput{ContactID: "ac-1.contact-1", PageToken: "next-1", PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].AccountingConnectionID != "ac-1" || page.NextPageToken != "next-2" {
		t.Fatalf("page = %#v", page)
	}
}

func TestInvoiceGetsOneRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/invoices/org-1/invoice/ac-1.invoice-1" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"ac-1.invoice-1","title":"INV-1","enabled":true}`))
	}))
	defer server.Close()

	client := New(server.URL, func() (string, error) { return "token", nil })
	invoice, err := client.Invoice(context.Background(), "org-1", "ac-1.invoice-1")
	if err != nil {
		t.Fatal(err)
	}
	if invoice.ID != "ac-1.invoice-1" || invoice.AccountingConnectionID != "ac-1" {
		t.Fatalf("invoice = %#v", invoice)
	}
}
