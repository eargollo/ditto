package server

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/db"
	"github.com/eargollo/ditto/internal/hash"
	"github.com/eargollo/ditto/internal/scan"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

const scanQueueCap = 64

type Server struct {
	cfg       *config.Config
	db        *sql.DB  // read-write (scan, hash, mutations)
	readDB    *sql.DB  // optional read-only pool; when set, use for read-heavy handlers so they don't block on writers
	mux       *http.ServeMux
	tmpl      *template.Template
	scanQueue chan int64 // scan IDs to process; one worker runs them serially
}

// NewServer creates a server. readDB is optional: if non-nil, read-only handlers use it so the UI stays responsive during scans (WAL allows concurrent readers).
func NewServer(cfg *config.Config, database *sql.DB, readDB *sql.DB) (*Server, error) {
	fm := template.FuncMap{
		"formatBytes": formatBytes,
	}
	tmpl, err := template.New("").Funcs(fm).ParseFS(fs.FS(templateFS), "templates/*.html")
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, db: database, readDB: readDB, mux: http.NewServeMux(), tmpl: tmpl, scanQueue: make(chan int64, scanQueueCap)}
	s.routes()
	return s, nil
}

// dbForRead returns the DB to use for read-only queries (readDB if set, else db).
func (s *Server) dbForRead() *sql.DB {
	if s.readDB != nil {
		return s.readDB
	}
	return s.db
}

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

func (s *Server) routes() {
	s.mux.HandleFunc("GET /{$}", s.handleHome())
	s.mux.HandleFunc("GET /scans", s.handleScans())
	s.mux.HandleFunc("GET /scans/roots", s.handleScanRootsList())
	s.mux.HandleFunc("POST /scans/roots", s.handleScanRootsAdd())
	s.mux.HandleFunc("POST /scans/start", s.handleScansStart())
	s.mux.HandleFunc("POST /scans/{id}/continue", s.handleScanContinue())
	s.mux.HandleFunc("GET /scans/{id}/status", s.handleScanStatus())
	s.mux.HandleFunc("GET /scans/{id}/duplicates/hash/{hash}", s.handleDuplicateHashGroup())
	s.mux.HandleFunc("GET /scans/{id}/duplicates/inode", s.handleDuplicateInodeGroup())
	s.mux.HandleFunc("GET /scans/{id}/duplicates", s.handleDuplicates())
	s.mux.HandleFunc("GET /scans/{id}", s.handleScanProgress())
	s.mux.HandleFunc("GET /api/fragment", s.handleFragment())
	s.mux.HandleFunc("GET /health", s.handleHealth())
	staticRoot, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticRoot))))
	s.mux.HandleFunc("/", s.handle404())
}

type pageData struct {
	Content template.HTML
	Data    interface{}
}

func (s *Server) renderPage(w http.ResponseWriter, layoutName, contentName string, data interface{}) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, contentName, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	page := pageData{Content: template.HTML(buf.Bytes()), Data: data}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, layoutName, page); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

const homePageSize = 20
const maxScansForRoots = 100
const homeMaxPathsPerGroup = 50 // limit paths loaded per group so home page stays fast
const homeListScansLimit = 300 // recent scans for dropdown (avoids loading huge scan table)

// ScanRootChoice is a root path with its latest scan id for the home dropdown.
type ScanRootChoice struct {
	RootPath   string
	ScanID     int64
	CreatedAt  time.Time
}

// GroupWithPaths is a duplicate group plus the file paths in it (for landing page).
type GroupWithPaths struct {
	Hash            string
	Count           int64
	Size            int64   // total group size (sum of file sizes)
	PerFileSize     int64   // size of each file (Size/Count) for human-readable "X MB each"
	Paths           []string
	PathsTruncated  bool    // true when only first N paths loaded for performance
}

// HomePageData is passed to the home template.
type HomePageData struct {
	Roots        []ScanRootChoice  // unique roots (latest scan per root) for dropdown
	SelectedScan int64             // scan id currently shown
	SelectedRoot string            // root path label
	Groups       []GroupWithPaths  // duplicate groups with file paths
	Page         int               // 1-based
	PageSize     int
	TotalGroups  int64
	TotalPages   int
	PrevPage     int               // 0 if no prev
	NextPage     int               // 0 if no next
}

func (s *Server) handleHome() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("[home] request started")
		defer func() { log.Printf("[home] served in %v", time.Since(start)) }()

		ctx := r.Context()
		scans, err := db.ListScansRecent(ctx, s.dbForRead(), homeListScansLimit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Build unique roots: latest scan per root_path (scans are newest first).
		seen := make(map[string]bool)
		var roots []ScanRootChoice
		for i := 0; i < len(scans) && len(roots) < maxScansForRoots; i++ {
			sc := scans[i]
			if seen[sc.RootPath] {
				continue
			}
			seen[sc.RootPath] = true
			roots = append(roots, ScanRootChoice{RootPath: sc.RootPath, ScanID: sc.ID, CreatedAt: sc.CreatedAt})
		}
		if len(roots) == 0 {
			s.renderPage(w, "layout.html", "home-content", HomePageData{Roots: roots})
			return
		}
		// Selected scan: from ?scan_id= or default first. 0 = "All (latest per folder)".
		selectedScanID := roots[0].ScanID
		if idStr := r.URL.Query().Get("scan_id"); idStr != "" {
			if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				if id == 0 {
					selectedScanID = 0
				} else {
					for _, r := range roots {
						if r.ScanID == id {
							selectedScanID = id
							break
						}
					}
				}
			}
		}
		var selectedRoot string
		if selectedScanID == 0 {
			selectedRoot = "All (latest per folder)"
		} else {
			for _, r := range roots {
				if r.ScanID == selectedScanID {
					selectedRoot = r.RootPath
					break
				}
			}
		}
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			if pn, err := strconv.Atoi(p); err == nil && pn >= 1 {
				page = pn
			}
		}
		scanIDsForAll := make([]int64, len(roots))
		for i := range roots {
			scanIDsForAll[i] = roots[i].ScanID
		}
		var totalGroups int64
		if selectedScanID == 0 {
			totalGroups, _ = db.DuplicateGroupsByHashCountAcrossScans(ctx, s.dbForRead(), scanIDsForAll)
		} else {
			totalGroups, _ = db.DuplicateGroupsByHashCount(ctx, s.dbForRead(), selectedScanID)
		}
		totalPages := 1
		if totalGroups > 0 && homePageSize > 0 {
			totalPages = int((totalGroups + int64(homePageSize) - 1) / int64(homePageSize))
		}
		if page > totalPages {
			page = totalPages
		}
		offset := (page - 1) * homePageSize
		var groups []db.DuplicateGroupByHash
		if selectedScanID == 0 {
			groups, _ = db.DuplicateGroupsByHashPaginatedAcrossScans(ctx, s.dbForRead(), scanIDsForAll, homePageSize, offset)
		} else {
			groups, _ = db.DuplicateGroupsByHashPaginated(ctx, s.dbForRead(), selectedScanID, homePageSize, offset)
		}
		// Attach file paths to each group (limit per group so home page stays fast)
		groupsWithPaths := make([]GroupWithPaths, 0, len(groups))
		for _, g := range groups {
			var files []db.File
			if selectedScanID == 0 {
				files, _ = db.FilesInHashGroupLimitAcrossScans(ctx, s.dbForRead(), scanIDsForAll, g.Hash, homeMaxPathsPerGroup)
			} else {
				files, _ = db.FilesInHashGroupLimit(ctx, s.dbForRead(), selectedScanID, g.Hash, homeMaxPathsPerGroup)
			}
			paths := make([]string, len(files))
			for i, f := range files {
				paths[i] = f.Path
			}
			perFile := int64(0)
			if g.Count > 0 {
				perFile = g.Size / g.Count
			}
			truncated := g.Count > int64(len(paths))
			groupsWithPaths = append(groupsWithPaths, GroupWithPaths{Hash: g.Hash, Count: g.Count, Size: g.Size, PerFileSize: perFile, Paths: paths, PathsTruncated: truncated})
		}
		prevPage, nextPage := 0, 0
		if page > 1 {
			prevPage = page - 1
		}
		if page < totalPages {
			nextPage = page + 1
		}
		data := HomePageData{
			Roots:        roots,
			SelectedScan: selectedScanID,
			SelectedRoot: selectedRoot,
			Groups:       groupsWithPaths,
			Page:         page,
			PageSize:     homePageSize,
			TotalGroups:  totalGroups,
			TotalPages:   totalPages,
			PrevPage:     prevPage,
			NextPage:     nextPage,
		}
		s.renderPage(w, "layout.html", "home-content", data)
	}
}

type scansPageData struct {
	Scans                  []db.Scan
	Roots                  []db.ScanRoot
	IncompleteScanIDByRoot map[string]int64 // root path -> latest incomplete scan id (for Continue per folder)
}

func (s *Server) handleScans() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		scans, _ := db.ListScans(ctx, s.dbForRead())
		roots, _ := db.ListScanRoots(ctx, s.dbForRead())
		byRoot := make(map[string]int64)
		for _, root := range roots {
			id, _ := db.GetLatestIncompleteScanForRoot(ctx, s.dbForRead(), root.Path)
			if id > 0 {
				byRoot[root.Path] = id
			}
		}
		s.renderPage(w, "layout.html", "scans-content", scansPageData{Scans: scans, Roots: roots, IncompleteScanIDByRoot: byRoot})
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
		if path == "" {
			rootIDStr := r.FormValue("root_id")
			if rootIDStr != "" {
				rootID, err := strconv.ParseInt(rootIDStr, 10, 64)
				if err != nil {
					http.Error(w, "invalid root_id", http.StatusBadRequest)
					return
				}
				root, err := db.GetScanRoot(r.Context(), s.db, rootID)
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
		scanRow, err := db.CreateScan(r.Context(), s.db, path)
		if err != nil {
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
			http.Error(w, "scan not found", http.StatusNotFound)
			return
		}
		s.renderPage(w, "layout.html", "scan-progress-content", sn)
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
			w.Write([]byte("<p>Scan not found.</p>"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.tmpl.ExecuteTemplate(w, "scan-status-fragment", sn); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
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
	ScanID  int64
	Inode   int64
	DeviceID *int64
	Files   []db.File
}

func (s *Server) handleDuplicates() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scanID, err := parseScanID(r.PathValue("id"))
		if err != nil {
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(roots)
	}
}

func (s *Server) handleScanRootsAdd() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "" && !strings.Contains(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") && !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			http.Error(w, "expect form", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/scans", http.StatusSeeOther)
	}
}

func (s *Server) handleFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<p class=\"text-gray-600\">Loaded via HTMX.</p>"))
	}
}

func (s *Server) handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.db != nil {
			if err := s.db.PingContext(r.Context()); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("db unhealthy"))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}

func (s *Server) handle404() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><body><h1>Not Found</h1></body></html>"))
	}
}

func (s *Server) Run(ctx context.Context) error {
	go s.runScanWorker(ctx)
	srv := &http.Server{
		Addr:         ":" + strconv.Itoa(s.cfg.Port()),
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
		if err := scan.RunScanForExisting(ctx, s.db, scanID, path, opts); err != nil {
			log.Printf("[scan] failed for scan %d: %v", scanID, err)
			return
		}
	}
	if err := hash.RunHashPhase(ctx, s.db, scanID, &hash.HashOptions{Workers: 6}); err != nil {
		log.Printf("[hash] background phase failed for scan %d: %v", scanID, err)
	}
}
