package outbound_test

import (
	"errors"
	"testing"

	"github.com/Baddysays/wernanmail/server/internal/outbound"
)

func TestDeliveryErrorPermanent(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&outbound.DeliveryError{Code: 550, Message: "user unknown"}, true},
		{&outbound.DeliveryError{Code: 450, Message: "try later"}, false},
		{&outbound.DeliveryError{Code: 421, Message: "timeout"}, false},
		{errors.New("550 5.1.1 NoSuchUser"), false}, // not typed — permanentFail in worker handles string
	}
	for _, c := range cases {
		de := outbound.AsDeliveryError(c.err)
		got := de != nil && de.Permanent()
		if got != c.want {
			t.Fatalf("%v: got %v want %v", c.err, got, c.want)
		}
	}
}

func TestIsTLSError(t *testing.T) {
	if !outbound.IsTLSError(&outbound.DeliveryError{TLS: true, Message: "x509"}) {
		t.Fatal("expected tls")
	}
	if outbound.IsTLSError(&outbound.DeliveryError{Code: 550, Message: "no"}) {
		t.Fatal("not tls")
	}
}
