package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"bitbucket.org/senprints/agent-office/internal/api"
	"bitbucket.org/senprints/agent-office/internal/setup"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/transfer"
)

func runCmd() *cobra.Command {
	var (
		addr           string
		origins        []string
		secureCookies  bool
		trustedProxies []string
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Bật server: API cho dashboard (scheduler và heartbeat sẽ thêm ở M1)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if addr == "" {
				addr = cfg.Server.APIAddr
			}
			origins = append(origins, cfg.Server.CORSOrigins...)

			h, err := resolveHome()
			if err != nil {
				return err
			}
			a, err := openApp(ctx, h)
			if err != nil {
				return err
			}
			defer a.Close()
			st := a.store

			proxies, err := parsePrefixes(trustedProxies)
			if err != nil {
				return err
			}
			log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
			handler := api.New(api.Config{
				Store: st, Auth: a.auth, AllowedOrigins: origins,
				SecureCookies: secureCookies, TrustedProxies: proxies, Logger: log, Version: version,
				Providers: a.providers, Org: a.org, Setup: setup.New(a.store, a.providers, a.org),
				Transfer: transfer.New(a.store, a.providers, a.org),
				Usage:    a.usage,
				Backup: func(ctx context.Context) (string, error) {
					return backupTo(ctx, a, filepath.Join(h.Dir, "backups", time.Now().Format("20060102-150405")))
				},
				System: api.SystemInfo{Mode: string(h.Mode), HomeDir: h.Dir, ProjectRoot: h.ProjectRoot},
			})

			if n, err := st.Users().Count(ctx); err == nil && n == 0 {
				fmt.Fprintln(os.Stderr, "Chưa có tài khoản nào. Tạo admin đầu tiên:\n  office user create --email you@company.com --role admin")
			}

			srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
			go sweepSessions(ctx, st, log)

			errCh := make(chan error, 1)
			go func() { errCh <- srv.ListenAndServe() }()
			fmt.Fprintf(os.Stderr, "office %s · chế độ %s · dữ liệu %s · api http://%s\n", version, h.Mode, h.Dir, addr)

			select {
			case err := <-errCh:
				if !errors.Is(err, http.ErrServerClosed) {
					return err
				}
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				return srv.Shutdown(shutdownCtx)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "api", "", "địa chỉ API (mặc định server.api_addr trong config, 127.0.0.1:8787)")
	cmd.Flags().StringSliceVar(&origins, "allowed-origin", []string{"http://localhost:3000", "http://127.0.0.1:3000"}, "origin của dashboard được phép gọi API")
	cmd.Flags().BoolVar(&secureCookies, "secure-cookies", false, "bật cờ Secure cho cookie (bắt buộc khi chạy sau HTTPS)")
	cmd.Flags().StringSliceVar(&trustedProxies, "trusted-proxy", []string{"127.0.0.1/32", "::1/128"}, "dải IP của proxy (dashboard Nuxt) được tin header X-Forwarded-*")
	return cmd
}

// sweepSessions deletes expired sessions hourly.
func sweepSessions(ctx context.Context, st storage.Store, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if n, err := st.Sessions().DeleteExpired(ctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Warn("sweep sessions", "err", err)
		} else if n > 0 {
			log.Info("sweep sessions", "deleted", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func parsePrefixes(in []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(in))
	for _, v := range in {
		p, err := netip.ParsePrefix(v)
		if err != nil {
			return nil, fmt.Errorf("--trusted-proxy %q: %w", v, err)
		}
		out = append(out, p)
	}
	return out, nil
}
