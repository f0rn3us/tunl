package commands

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/goji/httpauth"
	"github.com/gorilla/handlers"
	"github.com/pjvds/tunl/pkg/templates"
	"github.com/pjvds/tunl/pkg/tunnel"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

var HttpCommand = &cli.Command{
	Name: "http",
	Flags: []cli.Flag{
		CopyToClipboardFlag,
		&cli.BoolFlag{
			Name:  "access-log",
			Usage: "Print http requests in Apache Log format to stderr",
			Value: true,
		},
		&cli.StringFlag{
			Name:  "basic-auth",
			Usage: "Adds HTTP basic access authentication",
		},
		&cli.BoolFlag{
			Name:  "insecure",
			Usage: "Skip TLS verification for local address (this does not effect TLS between the tunl client and server or the public address)",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "qr",
			Usage: "Print QR code of the public address",
		},
	},
	ArgsUsage: "<url>",
	Usage:     "Expose a HTTP service via a public address",
	Action: func(ctx *cli.Context) error {
		var targetURL *url.URL
		target := ctx.Args().First()
		if len(target) == 0 {
			fmt.Fprint(os.Stderr, "You must specify the <url> argument\n\n")
			cli.ShowCommandHelpAndExit(ctx, ctx.Command.Name, 1)
		}

		if !strings.Contains(target, "://") {
			if strings.HasPrefix(target, ":") {
				target = target[1:]
			}

			if port, err := strconv.Atoi(target); err == nil {
				target = fmt.Sprintf("http://localhost:%v", port)
			} else {
				target = "http://" + target
			}
		}

		parsed, err := url.Parse(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid <url> argument value: %v\n\n", err)
			cli.ShowCommandHelpAndExit(ctx, ctx.Command.Name, 1)
		}
		targetURL = parsed

		proxy := httputil.NewSingleHostReverseProxy(targetURL)

		if ctx.Bool("insecure") {
			proxy.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
		}

		originalDirector := proxy.Director

		proxy.Director = func(request *http.Request) {
			originalDirector(request)
			request.Host = targetURL.Host
		}

		host := ctx.String("host")
		if len(host) == 0 {
			fmt.Fprint(os.Stderr, "Host cannot be empty\nSee --host flag for more information.\n\n")

			cli.ShowCommandHelpAndExit(ctx, ctx.Command.Name, 1)
			return cli.Exit("Host cannot be empty.", 1)
		}

		hostURL, err := url.Parse(host)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Host value invalid: %v\nSee --host flag for more information.\n\n", err)

			cli.ShowCommandHelpAndExit(ctx, ctx.Command.Name, 1)
			return nil
		}

		hostnameWithoutPort := hostURL.Hostname()
		if len(hostnameWithoutPort) == 0 {
			fmt.Fprintf(os.Stderr, "Host hostname cannot be empty, see --host flag for more information.\n\n")

			cli.ShowCommandHelpAndExit(ctx, ctx.Command.Name, 1)
			return nil
		}

		tunnel, err := tunnel.OpenHTTP(ctx.Context, zap.NewNop(), hostURL)
		if err != nil {
			return cli.Exit(err.Error(), 18)
		}

		PrintTunnel(ctx, tunnel.Address(), target)

		handler := handlers.LoggingHandler(os.Stdout, proxy)

		proxy.ErrorHandler = func(response http.ResponseWriter, request *http.Request, err error) {
			hostname, _ := os.Hostname()
			if len(hostname) == 0 {
				hostname = "<unknown>"
			}

			fmt.Println(err)

			var unwrapped = err

			for next := errors.Unwrap(unwrapped); next != nil; next = errors.Unwrap(unwrapped) {
				unwrapped = next
			}

			response.WriteHeader(http.StatusBadGateway)

			templates.HttpClientError(response, templates.HttpClientErrorInput{
				RemoteAddress:     tunnel.Address(),
				LocalHostname:     hostname,
				LocalAddress:      target,
				TunlClientVersion: ctx.App.Version,
				ErrMessage:        unwrapped.Error(),
				ErrType:           fmt.Sprintf("%T", err),
				ErrDetails:        err.Error(),
				Year:              time.Now().Year(),
			})
		}

		if basicAuth := ctx.String("basic-auth"); len(basicAuth) > 0 {
			split := strings.Split(basicAuth, ":")
			if len(split) != 2 {
				return cli.Exit("invalid basic-auth value", 1)
			}

			user := split[0]
			password := split[1]

			if len(user) == 0 {
				return cli.Exit("invalid basic-auth value: empty user", 1)
			}
			if len(password) == 0 {
				return cli.Exit("invalid basic-auth value: empty password", 1)
			}

			handler = httpauth.SimpleBasicAuth(user, password)(handler)
		}

		if err := http.Serve(tunnel, handler); err != nil {
			return cli.Exit("fatal error: "+err.Error(), 1)
		}

		return nil
	},
}
