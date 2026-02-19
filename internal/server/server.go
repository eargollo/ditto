package server

import (
	"bytes"
	"context"
	"embed"
	"encoding/csv"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/db"
	"github.com/eargollo/ditto/internal/hash"
	"github.com/eargollo/ditto/internal/scan"
	"github.com/eargollo/ditto/internal/version"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

const scanQueueCap = 64

type Server struct {
	cfg       *config.Config
	db        db.Database
	mux       *http.ServeMux
	tmpl      *template.Template
	scanQueue chan int64 // scan IDs to process; one worker runs them serially

	// When Run() is used with port 0, listenAddr is set to the base URL and listenReady is closed once listening.
	mu          sync.Mutex
	listenAddr  string       // e.g. "http://127.0.0.1:12345"
	listenReady chan struct{} // closed when server is listening (only set when port 0)
	listenCond  *sync.Cond    // signaled when listenReady is set (so ListenReady() can block until Run() sets it)
}

// NewServer creates a server using the given config and database.
func NewServer(cfg *config.Config, database db.Database) (*Server, error) {
	fm := template.FuncMap{
		"formatBytes":        formatBytes,
		"formatBytesWithRaw": formatBytesWithRaw,
	}
	tmpl, err := template.New("").Funcs(fm).ParseFS(fs.FS(templateFS), "templates/*.html")
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, db: database, mux: http.NewServeMux(), tmpl: tmpl, scanQueue: make(chan int64, scanQueueCap)}
	s.listenCond = sync.NewCond(&s.mu)
	s.routes()
	return s, nil
}

func (s *Server) dbForRead() db.Database { return s.db }

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	if exp >= len(units) {
		exp = len(units) - 1
		div = int64(1)
		for i := 0; i < exp; i++ {
			div *= unit
		}
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + units[exp]
}

// formatBytesWithRaw formats n as "X.XX GB (4,342,234,453 bytes)". Accepts *int64 for optional values; nil or zero returns "—".
func formatBytesWithRaw(n *int64) string {
	if n == nil {
		return "—"
	}
	v := *n
	if v == 0 {
		return "0 B (0 bytes)"
	}
	return formatBytes(v) + " (" + formatIntWithCommas(v) + " bytes)"
}

func formatIntWithCommas(n int64) string {
	if n < 0 {
		return "-" + formatIntWithCommas(-n)
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /{$}", s.handleHome())
	s.mux.HandleFunc("GET /scans", s.handleScans())
	s.mux.HandleFunc("GET /scans/roots", s.handleScanRootsList())
	s.mux.HandleFunc("POST /scans/roots", s.handleScanRootsAdd())
	s.mux.HandleFunc("POST /scans/start", s.handleScansStart())
	s.mux.HandleFunc("POST /scans/{id}/continue", s.handleScanContinue())
	s.mux.HandleFunc("GET /scans/{id}/status", s.handleScanStatus())
	s.mux.HandleFunc("GET /duplicates/hash/{hash}", s.handleDuplicateHashGroupByHash())
	s.mux.HandleFunc("GET /scans/{id}/duplicates/hash/{hash}", s.handleDuplicateHashGroup())
	s.mux.HandleFunc("GET /scans/{id}/duplicates/inode", s.handleDuplicateInodeGroup())
	s.mux.HandleFunc("GET /scans/{id}/duplicates", s.handleDuplicates())
	s.mux.HandleFunc("GET /scans/{id}/export", s.handleScanExport())
	s.mux.HandleFunc("GET /scans/{id}", s.handleScanProgress())
	s.mux.HandleFunc("GET /api/fragment", s.handleFragment())
	s.mux.HandleFunc("GET /health", s.handleHealth())

	// REST API (JSON); see docs/plan/rest-api-and-pages.md
	s.mux.HandleFunc("GET /api/health", s.apiHealth())
	s.mux.HandleFunc("GET /api/roots", s.apiRootsList())
	s.mux.HandleFunc("POST /api/roots", s.apiRootsCreate())
	s.mux.HandleFunc("GET /api/roots/{id}", s.apiRootsGet())
	s.mux.HandleFunc("GET /api/scans", s.apiScansList())
	s.mux.HandleFunc("POST /api/scans", s.apiScansCreate())
	s.mux.HandleFunc("GET /api/scans/{id}", s.apiScansGet())
	s.mux.HandleFunc("POST /api/scans/{id}/continue", s.apiScansContinue())
	s.mux.HandleFunc("GET /api/scans/{id}/status", s.apiScansStatus())
	s.mux.HandleFunc("GET /api/duplicates/summary", s.apiDuplicatesSummary())
	s.mux.HandleFunc("GET /api/duplicates/groups", s.apiDuplicatesGroups())

	staticRoot, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticRoot))))
	s.mux.HandleFunc("/", s.handle404())
}

type pageData struct {
	Content template.HTML
	Data    interface{}
	Version string
}

func (s *Server) renderPage(w http.ResponseWriter, layoutName, contentName string, data interface{}) {
	var contentBuf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&contentBuf, contentName, data); err != nil {
		log.Printf("error: render page content %q: %v", contentName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	page := pageData{Content: template.HTML(contentBuf.Bytes()), Data: data, Version: version.Version} // #nosec G203 -- content from our own templates, not user input
	var layoutBuf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&layoutBuf, layoutName, page); err != nil {
		log.Printf("error: render page layout %q: %v", layoutName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(layoutBuf.Bytes())
}

const homePageSize = 20
const maxScansForRoots = 100
const homeMaxPathsPerGroup = 50 // limit paths loaded per group so home page stays fast
const homeListScansLimit = 300  // recent scans for dropdown (avoids loading huge scan table)

// ScanRootChoice is a root path with its latest scan id for the home dropdown.
type ScanRootChoice struct {
	RootPath  string
	ScanID    int64
	CreatedAt time.Time
}

// GroupWithPaths is a duplicate group plus the file paths in it (for landing page).
type GroupWithPaths struct {
	Hash           string
	Count          int64
	Size           int64 // total group size (sum of file sizes)
	PerFileSize    int64 // size of each file (Size/Count) for human-readable "X MB each"
	Paths          []string
	PathsTruncated bool // true when only first N paths loaded for performance
}

// HomePageData is passed to the home template.
type HomePageData struct {
	Summary      db.DuplicateGroupsHashSummary // summary at top (groups, files, size that can be saved)
	Groups       []GroupWithPaths              // duplicate groups with file paths
	Page         int                           // 1-based
	PageSize     int
	TotalGroups  int64
	TotalPages   int
	PrevPage     int // 0 if no prev
	NextPage     int // 0 if no next
}

// shellData is passed to the shell template. ScanID is 0 for home/scans; set for scan progress so app.js can poll status.
type shellData struct {
	ScanID int64
}

func (s *Server) handleHome() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.renderPage(w, "layout.html", "shell-content", &shellData{})
	}
}

// handleDuplicateHashGroupByHash serves GET /duplicates/hash/{hash} using precomputed view (files by hash, deleted_at IS NULL).
func (s *Server) handleDuplicateHashGroupByHash() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hash := r.PathValue("hash")
		if hash == "" {
			http.Error(w, "hash required", http.StatusBadRequest)
			return
		}
		files, err := db.FilesInHashGroupByHash(r.Context(), s.dbForRead(), hash, 0)
		if err != nil {
			log.Printf("error: files in hash group hash=%s: %v", hash, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.renderPage(w, "layout.html", "duplicate-group-content", hashGroupData{ScanID: 0, Hash: hash, Files: files})
	}
}

type scansPageData struct {
	Scans                  []db.Scan
	Roots                  []db.ScanRoot
	IncompleteScanIDByRoot map[string]int64 // root path -> latest incomplete scan id (for Continue per folder)
}

func (s *Server) handleScans() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.renderPage(w, "layout.html", "shell-content", &shellData{})
	}
}

func (s *Server) handleScansStart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		path := strings.TrimSpace(r.FormValue("root_path"))
		var folderID int64
		if path == "" {
			rootIDStr := r.FormValue("root_id")
			if rootIDStr != "" {
				var err error
				folderID, err = strconv.ParseInt(rootIDStr, 10, 64)
				if err != nil {
					http.Error(w, "invalid root_id", http.StatusBadRequest)
					return
				}
				root, err := db.GetScanRoot(r.Context(), s.db, folderID)
				if err != nil {
					http.Error(w, "root not found", http.StatusNotFound)
					return
				}
				path = root.Path
			}
		}
		if path == "" {
			http.Error(w, "root_path or root_id required", http.StatusBadRequest)
			return
		}
		if folderID == 0 {
			var err error
			folderID, err = db.GetOrCreateFolderByPath(r.Context(), s.db, path)
			if err != nil {
				log.Printf("error: get or create folder: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		scanRow, err := db.CreateScan(r.Context(), s.db, folderID)
		if err != nil {
			log.Printf("error: create scan: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		scanID := scanRow.ID
		select {
		case s.scanQueue <- scanID:
			// queued
		default:
			http.Error(w, "scan queue is full, try again later", http.StatusServiceUnavailable)
			return
		}
		http.Redirect(w, r, "/scans/"+strconv.FormatInt(scanID, 10), http.StatusSeeOther)
	}
}

func (s *Server) handleScanContinue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		scanID, err := parseScanID(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		sn, err := db.GetScan(r.Context(), s.dbForRead(), scanID)
		if err != nil {
			http.Error(w, "scan not found", http.StatusNotFound)
			return
		}
		// Already fully complete: just go to progress page
		if sn.CompletedAt != nil && sn.HashCompletedAt != nil {
			http.Redirect(w, r, "/scans/"+strconv.FormatInt(scanID, 10), http.StatusSeeOther)
			return
		}
		// Return any files stuck in 'hashing' (from a cancelled run) to the queue so they get retried.
		if err := db.ResetHashStatusHashingToPending(r.Context(), s.db, scanID); err != nil {
			log.Printf("error: reset hash status for scan %d: %v", scanID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		select {
		case s.scanQueue <- scanID:
			// queued
		default:
			http.Error(w, "scan queue is full, try again later", http.StatusServiceUnavailable)
			return
		}
		http.Redirect(w, r, "/scans/"+strconv.FormatInt(scanID, 10), http.StatusSeeOther)
	}
}

func (s *Server) handleScanProgress() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scanID, err := parseScanID(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if _, err := db.GetScan(r.Context(), s.dbForRead(), scanID); err != nil {
			if isNotFound(err) {
				http.Error(w, "scan not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.renderPage(w, "layout.html", "shell-content", &shellData{ScanID: scanID})
	}
}

func (s *Server) handleScanStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		if idStr == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		scanID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		sn, err := db.GetScan(r.Context(), s.dbForRead(), scanID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("<p>Scan not found.</p>"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		var buf bytes.Buffer
		if err := s.tmpl.ExecuteTemplate(&buf, "scan-status-fragment", sn); err != nil {
			log.Printf("error: scan status fragment: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(buf.Bytes())
	}
}

func (s *Server) handleScanExport() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		if idStr == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		scanID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		if _, err := db.GetScan(ctx, s.dbForRead(), scanID); err != nil {
			http.Error(w, "scan not found", http.StatusNotFound)
			return
		}
		files, err := db.GetFilesByScanID(ctx, s.dbForRead(), scanID)
		if err != nil {
			log.Printf("error: export scan %d: %v", scanID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		var buf bytes.Buffer
		cw := csv.NewWriter(&buf)
		if err := cw.Write([]string{"path", "hash", "size"}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, f := range files {
			hashVal := ""
			if f.Hash != nil {
				hashVal = *f.Hash
			}
			// Normalize to forward slashes so CSV is comparable across OS (e.g. export on Linux vs reference on Windows).
			if err := cw.Write([]string{filepath.ToSlash(f.Path), hashVal, strconv.FormatInt(f.Size, 10)}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="scan-`+idStr+`-files.csv"`)
		// UTF-8 BOM so Excel and similar tools open the file with correct encoding
		_, _ = w.Write([]byte("\xef\xbb\xbf"))
		_, _ = w.Write(buf.Bytes())
	}
}

type duplicatesPageData struct {
	ScanID  int64
	ByHash  []db.DuplicateGroupByHash
	ByInode []db.DuplicateGroupByInode
}

type hashGroupData struct {
	ScanID           int64
	Hash             string
	Files            []db.File
	RootPathByScanID map[int64]string // when ScanID is 0 (All), root path per scan for display
}

type inodeGroupData struct {
	ScanID   int64
	Inode    int64
	DeviceID *int64
	Files    []db.File
}

func (s *Server) handleDuplicates() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scanID, err := parseScanID(r.PathValue("id"))
		if err != nil {
			log.Printf("error: duplicates parse scan id %q: %v", r.PathValue("id"), err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := db.GetScan(r.Context(), s.dbForRead(), scanID); err != nil {
			http.Error(w, "scan not found", http.StatusNotFound)
			return
		}
		byHash, _ := db.DuplicateGroupsByHash(r.Context(), s.dbForRead(), scanID)
		byInode, _ := db.DuplicateGroupsByInode(r.Context(), s.dbForRead(), scanID)
		s.renderPage(w, "layout.html", "duplicates-content", duplicatesPageData{ScanID: scanID, ByHash: byHash, ByInode: byInode})
	}
}

func (s *Server) handleDuplicateHashGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scanID, err := parseScanID(r.PathValue("id"))
		if err != nil {
			log.Printf("error: duplicate hash group parse scan id %q: %v", r.PathValue("id"), err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		hash := r.PathValue("hash")
		if hash == "" {
			http.Error(w, "hash required", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		database := s.dbForRead()
		if scanID == 0 {
			// "All (latest per folder)": use latest scan per root
			scans, _ := db.ListScansRecent(ctx, database, homeListScansLimit)
			seen := make(map[string]bool)
			var scanIDs []int64
			for _, sc := range scans {
				if seen[sc.RootPath] {
					continue
				}
				seen[sc.RootPath] = true
				scanIDs = append(scanIDs, sc.ID)
			}
			files, _ := db.FilesInHashGroupAcrossScans(ctx, database, scanIDs, hash)
			rootByScan := make(map[int64]string)
			for _, sc := range scans {
				if _, ok := rootByScan[sc.ID]; !ok {
					rootByScan[sc.ID] = sc.RootPath
				}
			}
			s.renderPage(w, "layout.html", "duplicate-group-content", hashGroupData{ScanID: 0, Hash: hash, Files: files, RootPathByScanID: rootByScan})
			return
		}
		if _, err := db.GetScan(ctx, database, scanID); err != nil {
			http.Error(w, "scan not found", http.StatusNotFound)
			return
		}
		files, err := db.FilesInHashGroup(r.Context(), database, scanID, hash)
		if err != nil {
			log.Printf("error: files in hash group scan=%d hash=%s: %v", scanID, hash, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.renderPage(w, "layout.html", "duplicate-group-content", hashGroupData{ScanID: scanID, Hash: hash, Files: files})
	}
}

func (s *Server) handleDuplicateInodeGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scanID, err := parseScanID(r.PathValue("id"))
		if err != nil {
			log.Printf("error: duplicate inode group parse scan id %q: %v", r.PathValue("id"), err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		inode, err := strconv.ParseInt(r.URL.Query().Get("inode"), 10, 64)
		if err != nil {
			http.Error(w, "inode required", http.StatusBadRequest)
			return
		}
		var deviceID *int64
		if d := r.URL.Query().Get("device_id"); d != "" {
			v, err := strconv.ParseInt(d, 10, 64)
			if err != nil {
				http.Error(w, "invalid device_id", http.StatusBadRequest)
				return
			}
			deviceID = &v
		}
		if _, err := db.GetScan(r.Context(), s.dbForRead(), scanID); err != nil {
			http.Error(w, "scan not found", http.StatusNotFound)
			return
		}
		files, err := db.FilesInInodeGroup(r.Context(), s.dbForRead(), scanID, inode, deviceID)
		if err != nil {
			log.Printf("error: files in inode group scan=%d: %v", scanID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.renderPage(w, "layout.html", "duplicate-inode-group-content", inodeGroupData{ScanID: scanID, Inode: inode, DeviceID: deviceID, Files: files})
	}
}

func parseScanID(idStr string) (int64, error) {
	return strconv.ParseInt(idStr, 10, 64)
}

func (s *Server) handleScanRootsList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roots, err := db.ListScanRoots(r.Context(), s.dbForRead())
		if err != nil {
			log.Printf("error: list scan roots: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(roots)
	}
}

func (s *Server) handleScanRootsAdd() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "" && !strings.Contains(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") && !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			http.Error(w, "expect form", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			log.Printf("error: parse form (scan roots add): %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		path := strings.TrimSpace(r.FormValue("path"))
		if path == "" {
			http.Error(w, "path required", http.StatusBadRequest)
			return
		}
		_, err := db.AddScanRoot(r.Context(), s.db, path)
		if err != nil {
			log.Printf("error: add scan root %q: %v", path, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/scans", http.StatusSeeOther)
	}
}

func (s *Server) handleFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<p class=\"text-gray-600\">Loaded via HTMX.</p>"))
	}
}

func (s *Server) handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.db != nil {
			if err := s.db.PingContext(r.Context()); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("db unhealthy"))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

func (s *Server) handle404() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><h1>Not Found</h1></body></html>"))
	}
}

// ListenAddr returns the base URL (e.g. "http://127.0.0.1:12345") once the server is listening.
// Only set when Run() is started with port 0. Callers should wait on ListenReady() first.
func (s *Server) ListenAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listenAddr
}

// ListenReady returns a channel that is closed when the server is listening. When port is 0,
// blocks until Run() has set the channel, then returns it so callers can wait for Serve() to start.
// When port is not 0, returns a channel that is already closed so callers do not block.
func (s *Server) ListenReady() <-chan struct{} {
	s.mu.Lock()
	for s.listenReady == nil {
		s.listenCond.Wait()
	}
	ready := s.listenReady
	s.mu.Unlock()
	if ready == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return ready
}

func (s *Server) Run(ctx context.Context) error {
	go s.runScanWorker(ctx)
	srv := &http.Server{
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	port := s.cfg.Port()
	if port == 0 {
		s.mu.Lock()
		s.listenReady = make(chan struct{})
		s.listenCond.Broadcast()
		s.mu.Unlock()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		addr := listener.Addr().(*net.TCPAddr)
		baseURL := "http://127.0.0.1:" + strconv.Itoa(addr.Port)
		s.mu.Lock()
		s.listenAddr = baseURL
		close(s.listenReady)
		s.mu.Unlock()
		return srv.Serve(listener)
	}

	addr := "0.0.0.0:" + strconv.Itoa(port)
	srv.Addr = addr
	log.Printf("Listening on http://%s", addr)
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// runScanWorker processes one scan at a time from the queue. Scans are serialized to avoid SQLITE_BUSY.
func (s *Server) runScanWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case scanID, ok := <-s.scanQueue:
			if !ok {
				return
			}
			s.runOneScan(ctx, scanID)
		}
	}
}

// runOneScan runs the scan phase (if needed) and hash phase for the given scan. Used by the serialized worker.
func (s *Server) runOneScan(ctx context.Context, scanID int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[scan] panic for scan %d: %v", scanID, r)
		}
	}()
	sn, err := db.GetScan(ctx, s.db, scanID)
	if err != nil {
		log.Printf("[scan] scan %d not found: %v", scanID, err)
		return
	}
	path := sn.RootPath
	opts, _ := scan.OptionsForRoot(path)
	log.Printf("[scan] started for scan %d path %s", scanID, path)
	if sn.CompletedAt == nil {
		if err := scan.RunScanForExisting(ctx, s.db, scanID, sn.FolderID, path, opts); err != nil {
			log.Printf("[scan] failed for scan %d: %v", scanID, err)
			return
		}
	}
	if err := hash.RunHashPhase(ctx, s.db, scanID, &hash.HashOptions{Workers: 6}); err != nil {
		log.Printf("[hash] background phase failed for scan %d: %v", scanID, err)
	}
}
