package cmd

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/spf13/cobra"
)

var (
	servePort string
	serveDir  string
)

// serveCmd represents the serve command.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "启动 API Server",
	Long: `启动 House of Cards HTTP API 服务器。

默认监听 8080 端口。

API 端点：
  GET    /api/v1/sessions           列出所有会期
  POST   /api/v1/sessions           创建新会期
  GET    /api/v1/sessions/:id      查看会期详情
  GET    /api/v1/ministers          列出所有部长
  POST   /api/v1/ministers/:id/summon  传召部长
  POST   /api/v1/bills/:id/assign  分配议案
  GET    /api/v1/gazettes           列出公报
  POST   /api/v1/webhooks           Webhook 端点

示例：
  hoc serve              # 启动服务器（默认 8080）
  hoc serve --port 9000 # 指定端口`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		// Don't defer db.Close() - keep it open for the server lifetime.

		addr := ":" + servePort
		fmt.Printf("🏛  启动 House of Cards API Server\n")
		fmt.Printf("   地址: http://localhost%s\n", addr)
		fmt.Printf("   按 Ctrl+C 停止\n")

		// Create HTTP mux.
		mux := http.NewServeMux()
		registerAPIRoutes(mux)

		// Create server.
		srv := &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		}

		// Start server in goroutine.
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("API server error", "err", err)
			}
		}()

		// Wait for interrupt signal.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		fmt.Println("\n⏹  关闭 API Server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}

		_ = db.Close()
		fmt.Println("✓ 服务器已关闭")
		return nil
	},
}

//nolint:gochecknoinits // Cobra convention: register flags in init().
func init() {
	serveCmd.Flags().StringVar(&servePort, "port", "8080", "API Server 监听端口")
	serveCmd.Flags().StringVar(&serveDir, "dir", "", "工作目录（默认为 ~/.hoc）")

	rootCmd.AddCommand(serveCmd)
}

// registerAPIRoutes registers all API routes.
func registerAPIRoutes(mux *http.ServeMux) {
	// Health check.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		writeJSON(w, map[string]string{"status": "ok"})
	})

	// API v1 routes.
	api := http.NewServeMux()

	// Sessions.
	api.HandleFunc("/sessions", handleSessions)
	api.HandleFunc("/sessions/", handleSessionDetail)

	// Ministers.
	api.HandleFunc("/ministers", handleMinisters)
	api.HandleFunc("/ministers/", handleMinisterAction)

	// Bills.
	api.HandleFunc("/bills/", handleBillAction)

	// Gazettes.
	api.HandleFunc("/gazettes", handleGazettes)

	// Webhooks.
	api.HandleFunc("/webhooks", handleWebhooks)

	mux.Handle("/api/v1/", api)
}

// ─── Handlers ───────────────────────────────────────────────────────────────

func handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		sessions, err := db.ListSessions()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, sessions)

	case "POST":
		var req struct {
			Title    string   `json:"title"`
			Topology string   `json:"topology"`
			Projects []string `json:"projects"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if req.Title == "" {
			writeError(w, http.StatusBadRequest, "title is required")
			return
		}

		if req.Topology == "" {
			req.Topology = "parallel"
		}

		// Build projects JSON.
		projectsJSON := "[]"
		if len(req.Projects) > 0 {
			b, err := json.Marshal(req.Projects)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid projects: "+err.Error())
				return
			}
			projectsJSON = string(b)
		}

		sid := fmt.Sprintf("session-%x", time.Now().UnixNano())
		sess := &store.Session{
			ID:       sid,
			Title:    req.Title,
			Topology: req.Topology,
			Projects: store.NullString(projectsJSON),
			Status:   "active",
		}
		if err := db.CreateSession(sess); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, map[string]interface{}{
			"id":       sid,
			"title":    req.Title,
			"topology": req.Topology,
			"status":   "active",
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/v1/sessions/{id}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "session ID required")
		return
	}

	switch r.Method {
	case "GET":
		sess, err := db.GetSession(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}

		bills, err := db.ListBillsBySession(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]interface{}{
			"session": sess,
			"bills":   bills,
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleMinisters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		ministers, err := db.ListMinisters()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, ministers)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleMinisterAction(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/v1/ministers/{id}/summon
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/ministers/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "minister ID required")
		return
	}
	ministerID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch r.Method {
	case "POST":
		if action == "summon" {
			var req struct {
				BillID string `json:"bill_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}

			minister, err := db.GetMinister(ministerID)
			if err != nil {
				writeError(w, http.StatusNotFound, "minister not found")
				return
			}

			if minister.Status != "idle" && minister.Status != "offline" {
				writeError(w, http.StatusConflict, fmt.Sprintf("minister status is %q, must be idle or offline", minister.Status))
				return
			}

			if err := db.UpdateMinisterStatus(ministerID, "working"); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

			if err := db.RecordEvent("minister.summoned", "api", req.BillID, ministerID, "", ""); err != nil {
				slog.Warn("failed to record event", "minister_id", ministerID, "err", err)
			}

			writeJSON(w, map[string]string{
				"status":      "summoned",
				"minister_id": ministerID,
				"bill_id":     req.BillID,
			})
			return
		}
		writeError(w, http.StatusBadRequest, "unknown action")

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleBillAction(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/v1/bills/{id}/assign
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/bills/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "bill ID required")
		return
	}
	billID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch r.Method {
	case "POST":
		if action == "assign" {
			var req struct {
				MinisterID string `json:"minister_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}

			if err := db.AssignBill(billID, req.MinisterID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if err := db.UpdateBillStatus(billID, "reading"); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

			writeJSON(w, map[string]string{
				"status":      "assigned",
				"bill_id":     billID,
				"minister_id": req.MinisterID,
			})
			return
		}

		if action == "enacted" {
			var req struct {
				Quality  float64 `json:"quality"`
				Notes    string  `json:"notes"`
				Duration int     `json:"duration"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}

			b, err := db.GetBill(billID)
			if err != nil {
				writeError(w, http.StatusNotFound, "bill not found")
				return
			}

			if err := db.UpdateBillStatus(billID, "enacted"); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

			ministerID := b.Assignee.String
			if ministerID != "" {
				h := &store.Hansard{
					ID:         shortID("hansard"),
					MinisterID: ministerID,
					BillID:     billID,
					Outcome:    store.NullString("enacted"),
					DurationS:  req.Duration,
					Quality:    req.Quality,
					Notes:      store.NullString(req.Notes),
				}
				if err := db.CreateHansard(h); err != nil {
					slog.Warn("failed to create hansard", "bill_id", billID, "err", err)
				}
			}

			summary := fmt.Sprintf("议案 [%s] \"%s\" 已通过（Enacted）", billID, b.Title)
			if req.Notes != "" {
				summary += "。备注：" + req.Notes
			}
			g := &store.Gazette{
				ID:           shortID("gazette"),
				FromMinister: store.NullString(ministerID),
				BillID:       store.NullString(billID),
				Type:         store.NullString("completion"),
				Summary:      summary,
			}
			if err := db.CreateGazette(g); err != nil {
				slog.Warn("failed to create gazette", "bill_id", billID, "err", err)
			}

			writeJSON(w, map[string]string{
				"status":  "enacted",
				"bill_id": billID,
			})
			return
		}

		writeError(w, http.StatusBadRequest, "unknown action")

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleGazettes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		ministerID := r.URL.Query().Get("minister")
		var gazettes []*store.Gazette
		var err error

		if ministerID != "" {
			gazettes, err = db.ListGazettesForMinister(ministerID)
		} else {
			gazettes, err = db.ListGazettes()
		}

		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, gazettes)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleWebhooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		// Read body for HMAC verification.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read body")
			return
		}

		// HMAC-SHA256 verification if secret is configured.
		secret := os.Getenv("HOC_WEBHOOK_SECRET")
		if secret != "" {
			sig := r.Header.Get("X-Hub-Signature-256")
			if sig == "" {
				writeError(w, http.StatusUnauthorized, "missing X-Hub-Signature-256")
				return
			}
			if !verifyWebhookSignature(body, sig, secret) {
				writeError(w, http.StatusUnauthorized, "invalid signature")
				return
			}
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if err := db.RecordEvent("webhook.received", "api", "", "", "", ""); err != nil {
			slog.Warn("failed to record webhook event", "err", err)
		}

		eventType := r.Header.Get("X-GitHub-Event")
		switch eventType {
		case "push":
			// Extract commit info and create a bill.
			commits, _ := payload["commits"].([]interface{})
			if len(commits) > 0 {
				firstCommit, _ := commits[0].(map[string]interface{})
				message, _ := firstCommit["message"].(string)
				if message != "" {
					billID := shortID("bill")
					b := &store.Bill{
						ID:        billID,
						Title:     truncate(message, 100),
						Status:    "draft",
						DependsOn: store.NullString("[]"),
					}
					if err := db.CreateBill(b); err != nil {
						writeError(w, http.StatusInternalServerError, "failed to create bill: "+err.Error())
						return
					}
					if err := db.RecordEvent("bill.created", "webhook", billID, "", "", `{"event":"push"}`); err != nil {
						slog.Warn("failed to record event", "bill_id", billID, "err", err)
					}
				}
			}

			writeJSON(w, map[string]string{
				"status": "processed",
				"event":  "push",
			})

		case "pull_request":
			pr, _ := payload["pull_request"].(map[string]interface{})
			title, _ := pr["title"].(string)
			if title == "" {
				title = "PR Review"
			}
			billID := shortID("bill")
			b := &store.Bill{
				ID:          billID,
				Title:       "Review: " + truncate(title, 90),
				Description: store.NullString("PR review from webhook"),
				Status:      "draft",
				DependsOn:   store.NullString("[]"),
			}
			if err := db.CreateBill(b); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create bill: "+err.Error())
				return
			}
			if err := db.RecordEvent("bill.created", "webhook", billID, "", "", `{"event":"pull_request"}`); err != nil {
				slog.Warn("failed to record event", "bill_id", billID, "err", err)
			}

			writeJSON(w, map[string]string{
				"status":  "processed",
				"event":   "pull_request",
				"bill_id": billID,
			})

		default:
			writeJSON(w, map[string]string{
				"status":  "received",
				"event":   eventType,
				"message": "event type not handled, acknowledged",
			})
		}

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// verifyWebhookSignature checks HMAC-SHA256 signature for GitHub webhooks.
func verifyWebhookSignature(body []byte, signature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, v interface{}) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("failed to write JSON", "err", err)
	}
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	writeJSON(w, map[string]string{"error": message})
}
