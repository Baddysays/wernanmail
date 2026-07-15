package outbound

import (
	"errors"
	"fmt"
	"strings"
)

// DeliveryError is a typed SMTP delivery failure.
type DeliveryError struct {
	Code    int
	Host    string
	Message string
	TLS     bool // true when failure is TLS/x509 related
	Partial []string // recipients that were accepted before failure (for retry of remainder)
}

func (e *DeliveryError) Error() string {
	if e.Code > 0 {
		return fmt.Sprintf("%d %s", e.Code, e.Message)
	}
	if e.TLS {
		return "tls: " + e.Message
	}
	return e.Message
}

func (e *DeliveryError) Permanent() bool {
	return e.Code >= 500 && e.Code < 600
}

func IsTLSError(err error) bool {
	var de *DeliveryError
	if errors.As(err, &de) && de.TLS {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "tls:") || strings.Contains(msg, "x509:") || strings.Contains(msg, "certificate")
}

func AsDeliveryError(err error) *DeliveryError {
	var de *DeliveryError
	if errors.As(err, &de) {
		return de
	}
	return nil
}
