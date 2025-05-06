package outbound

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/metacubex/mihomo/component/proxydialer"
	"strings"

	"io"
	"net"
	"strconv"

	"github.com/metacubex/mihomo/component/ca"
	"github.com/metacubex/mihomo/component/dialer"
	C "github.com/metacubex/mihomo/constant"
)

type Bdzl struct {
	*Base
	user      string
	pass      string
	tlsConfig *tls.Config
	option    *BdzlOption
}

type BdzlOption struct {
	BasicOption
	Name           string            `proxy:"name"`
	Server         string            `proxy:"server"`
	Port           int               `proxy:"port"`
	UserName       string            `proxy:"username,omitempty"`
	Password       string            `proxy:"password,omitempty"`
	TLS            bool              `proxy:"tls,omitempty"`
	SNI            string            `proxy:"sni,omitempty"`
	SkipCertVerify bool              `proxy:"skip-cert-verify,omitempty"`
	Fingerprint    string            `proxy:"fingerprint,omitempty"`
	Headers        map[string]string `proxy:"headers,omitempty"`
}

// StreamConn implements C.ProxyAdapter
func (h *Bdzl) StreamConnContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (net.Conn, error) {
	if h.tlsConfig != nil {
		cc := tls.Client(c, h.tlsConfig)
		//ctx, cancel := context.WithTimeout(context.Background(), C.DefaultTLSTimeout)
		//defer cancel()
		err := cc.HandshakeContext(ctx)
		c = cc
		if err != nil {
			return nil, fmt.Errorf("%s connect error: %w", h.addr, err)
		}
	}

	if err := h.shakeHand(metadata, c); err != nil {
		return nil, err
	}
	return c, nil
}

// DialContext implements C.ProxyAdapter
func (h *Bdzl) DialContext(ctx context.Context, metadata *C.Metadata) (_ C.Conn, err error) {
	return h.DialContextWithDialer(ctx, dialer.NewDialer(h.DialOptions()...), metadata)
}

// DialContextWithDialer implements C.ProxyAdapter
func (h *Bdzl) DialContextWithDialer(ctx context.Context, dialer C.Dialer, metadata *C.Metadata) (_ C.Conn, err error) {
	if len(h.option.DialerProxy) > 0 {
		dialer, err = proxydialer.NewByName(h.option.DialerProxy, dialer)
		if err != nil {
			return nil, err
		}
	}
	c, err := dialer.DialContext(ctx, "tcp", h.addr)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", h.addr, err)
	}

	defer func(c net.Conn) {
		safeConnClose(c, err)
	}(c)

	c, err = h.StreamConnContext(ctx, c, metadata)
	if err != nil {
		return nil, err
	}

	return NewConn(c, h), nil
}

// SupportWithDialer implements C.ProxyAdapter
func (h *Bdzl) SupportWithDialer() C.NetWork {
	return C.TCP
}

func (h *Bdzl) shakeHand(metadata *C.Metadata, rw io.ReadWriter) error {
	addr := metadata.RemoteAddress()
	header := "CONNECT " + addr + "HTTP/1.1\r\n"
	//增加headers
	if len(h.option.Headers) != 0 {
		for key, value := range h.option.Headers {
			header += key + ": " + value + "\r\n"
		}
	}
	if h.user != "" && h.pass != "" {
		auth := h.user + ":" + h.pass
		header += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
	}

	header += "\r\n"

	total, err := rw.Write([]byte(header))

	if err != nil {
		return nil
	}
	rd := make([]byte, total)

	if _, err := rw.Read(rd); err == nil {
		line := strings.Split(string(rd), "\n")[0]
		httpStatus := strings.Split(line, " ")[1]
		switch httpStatus {
		case "200":
			return nil
		case "407":
			return errors.New("HTTP need auth")
		case "405":
			return errors.New("CONNECT method not allowed by proxy")
		default:
			return errors.New(string(rd))
		}
	} else {
		return nil
	}

	return fmt.Errorf("can not connect remote err code: %s", string(rd))
}

func NewBdzl(option BdzlOption) (*Bdzl, error) {
	var tlsConfig *tls.Config
	if option.TLS {
		sni := option.Server
		if option.SNI != "" {
			sni = option.SNI
		}
		var err error
		tlsConfig, err = ca.GetSpecifiedFingerprintTLSConfig(&tls.Config{
			InsecureSkipVerify: option.SkipCertVerify,
			ServerName:         sni,
		}, option.Fingerprint)
		if err != nil {
			return nil, err
		}
	}

	return &Bdzl{
		Base: &Base{
			name:   option.Name,
			addr:   net.JoinHostPort(option.Server, strconv.Itoa(option.Port)),
			tp:     C.Bdzl,
			tfo:    option.TFO,
			iface:  option.Interface,
			rmark:  option.RoutingMark,
			prefer: C.NewDNSPrefer(option.IPVersion),
		},
		user:      option.UserName,
		pass:      option.Password,
		tlsConfig: tlsConfig,
		option:    &option,
	}, nil
}
