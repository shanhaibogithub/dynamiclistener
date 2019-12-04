package server

import (
	"context"
	"crypto"
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/rancher/dynamiclistener"
	"github.com/rancher/dynamiclistener/factory"
	"github.com/rancher/dynamiclistener/storage/memory"
	"github.com/sirupsen/logrus"
)

type ListenOpts struct {
	CA      *x509.Certificate
	CAKey   crypto.Signer
	Storage dynamiclistener.TLSStorage
}

func ListenAndServe(ctx context.Context, httpsPort, httpPort int, handler http.Handler, opts *ListenOpts) error {
	var (
		// https listener will change this if http is enabled
		targetHandler = handler
	)

	if opts == nil {
		opts = &ListenOpts{}
	}

	if httpsPort > 0 {
		var (
			caCert *x509.Certificate
			caKey  crypto.Signer
			err    error
		)

		if opts.CA != nil && opts.CAKey != nil {
			caCert, caKey = opts.CA, opts.CAKey
		} else {
			caCert, caKey, err = factory.LoadOrGenCA()
			if err != nil {
				return err
			}
		}

		tlsTCPListener, err := dynamiclistener.NewTCPListener("0.0.0.0", httpsPort)
		if err != nil {
			return err
		}

		storage := opts.Storage
		if storage == nil {
			storage = memory.New()
		}

		dynListener, dynHandler, err := dynamiclistener.NewListener(tlsTCPListener, storage, caCert, caKey, dynamiclistener.Config{})
		if err != nil {
			return err
		}

		targetHandler = wrapHandler(dynHandler, handler)
		tlsServer := http.Server{
			Handler: targetHandler,
		}
		targetHandler = dynamiclistener.HTTPRedirect(targetHandler)

		go func() {
			logrus.Infof("Listening on 0.0.0.0:%d", httpsPort)
			err := tlsServer.Serve(dynListener)
			if err != http.ErrServerClosed && err != nil {
				logrus.Fatalf("https server failed: %v", err)
			}
		}()
		go func() {
			<-ctx.Done()
			tlsServer.Shutdown(context.Background())
		}()
	}

	if httpPort > 0 {
		httpServer := http.Server{
			Addr:    fmt.Sprintf("0.0.0.0:%d", httpPort),
			Handler: targetHandler,
		}
		go func() {
			logrus.Infof("Listening on 0.0.0.0:%d", httpPort)
			err := httpServer.ListenAndServe()
			if err != http.ErrServerClosed && err != nil {
				logrus.Fatalf("http server failed: %v", err)
			}
		}()
		go func() {
			<-ctx.Done()
			httpServer.Shutdown(context.Background())
		}()
	}

	return nil
}

func wrapHandler(handler http.Handler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		handler.ServeHTTP(rw, req)
		next.ServeHTTP(rw, req)
	})
}
