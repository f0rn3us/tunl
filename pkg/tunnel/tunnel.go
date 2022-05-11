package tunnel

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/Masterminds/semver"
	"github.com/f0rn3us/tunl/pkg/tunnel/client"
	"github.com/f0rn3us/tunl/pkg/tunnel/state"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (t *tunnel) Disconnected(err error) (*state.Disconnected, error) {
	return &state.Disconnected{LastErr: err, ReconnectCreator: t}, nil
}

type TokenRefresher interface {
	SetToken(token string)
}

func (t *tunnel) SetTunnelInfo(info client.TunnelInfo) {
	t.id = info.Id
	t.token = info.Token
	t.address = info.Address

	// if we got a version, try to announce it
	if version := info.Version; version != nil {
		go func() {
			select {
			case t.versions <- *info.Version:
			case <-time.After(1 * time.Second):
			}
		}()
	}
}

func (t *tunnel) SetToken(token string) {
	t.token = token
}
func (t *tunnel) Running(conn net.Conn) (*state.Running, error) {
	return &state.Running{
		Conn:                conn,
		Accepted:            t.accepted,
		DisconnectedCreator: t,
	}, nil
}

var ErrClosed = errors.New("tunnel closed")

func IsClosed(err error) bool {
	return err == ErrClosed
}

type Tunnel interface {
	net.Listener
	Address() string

	StateChanges() <-chan string
	NewVersions() <-chan semver.Version
}

func (t *tunnel) Close() error {
	// noop
	// TODO: close?
	return nil
}

func (t *tunnel) Addr() net.Addr {
	return nil
}

func (t *tunnel) Address() string {
	return t.address
}

func (t *tunnel) Accept() (net.Conn, error) {
	select {
	case <-t.done:
		return nil, t.err
	case conn := <-t.accepted:
		return conn, nil
	}
}

type tunnel struct {
	log      *zap.Logger
	t        client.TunnelType
	host     *url.URL
	server   client.ServerInfo
	conn     net.Conn
	changes  chan string
	versions chan semver.Version

	id       string
	token    string
	address  string
	accepted chan net.Conn
	err      error
	done     chan struct{}
	ctx      context.Context
}

func (t *tunnel) Reconnect(attempt int) (*state.Reconnect, error) {
	return &state.Reconnect{
		Server: t.server,
		Type:   t.t,
		Token:  t,
		Tunnel: client.TunnelInfo{
			Id:      t.id,
			Address: t.address,
			Token:   t.token,
		},
		Attempt:          attempt,
		ReconnectCreator: t,
		RunningCreator:   t,
	}, nil
}

func (t *tunnel) StateChanges() <-chan string {
	return t.changes
}

func (t *tunnel) NewVersions() <-chan semver.Version {
	return t.versions
}

func OpenTCP(ctx context.Context, log *zap.Logger, host *url.URL) (Tunnel, error) {
	return open(ctx, log, host, client.TypeTCP)
}

func OpenHTTP(ctx context.Context, log *zap.Logger, host *url.URL) (Tunnel, error) {
	return open(ctx, log, host, client.TypeHTTP)
}

func open(ctx context.Context, log *zap.Logger, host *url.URL, t client.TunnelType) (Tunnel, error) {
	server, err := client.ParseHostURL(host)
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})
	accepted := make(chan net.Conn)
	changes := make(chan string, 10)
	versions := make(chan semver.Version)

	tunnel := &tunnel{
		host:     host,
		server:   server,
		accepted: accepted,
		done:     done,
		changes:  changes,
		versions: versions,
		t:        t,
	}

	connect := &state.Connect{
		Server:           server,
		Type:             t,
		TunnelInfoSetter: tunnel,
		RunningCreator:   tunnel,
	}

	state, err := connect.Run(ctx, log)
	if err != nil {
		return nil, errors.Wrap(err, "tunl server connection failed")
	}

	go func() {
		defer close(done)
		defer close(accepted)

		for {
			state, err = state.Run(ctx, log)
			if err != nil {
				tunnel.err = err
				return
			}

			if status := state.String(); len(status) > 0 {
				select {
				case tunnel.changes <- state.String():
				default:
				}
			}
		}
	}()

	return tunnel, nil
}
