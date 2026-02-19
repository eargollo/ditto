package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/eargollo/ditto/internal/db"
)

// API response/request DTOs with JSON tags for REST convention (snake_case where desired).

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: encode JSON: %v", err)
	}
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// Root (scan root) API types.
type apiRoot struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	CreatedAt string `json:"created_at"`
}

func rootToAPI(r *db.ScanRoot) apiRoot {
	return apiRoot{ID: r.ID, Path: r.Path, CreatedAt: r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
}

// Scan API types (nullable fields as pointers for JSON omitempty).
type apiScan struct {
	ID                  int64   `json:"id"`
	FolderID            int64   `json:"folder_id"`
	RootPath            string  `json:"root_path"`
	CreatedAt           string  `json:"created_at"`
	CompletedAt         *string `json:"completed_at,omitempty"`
	HashStartedAt       *string `json:"hash_started_at,omitempty"`
	HashCompletedAt     *string `json:"hash_completed_at,omitempty"`
	FileCount           *int64  `json:"file_count,omitempty"`
	ScanSkippedCount    *int64  `json:"scan_skipped_count,omitempty"`
	HashedFileCount     *int64  `json:"hashed_file_count,omitempty"`
	HashedByteCount     *int64  `json:"hashed_byte_count,omitempty"`
	HashReusedCount     *int64  `json:"hash_reused_count,omitempty"`
	HashErrorCount      *int64  `json:"hash_error_count,omitempty"`
}

func scanToAPI(s *db.Scan) apiScan {
	out := apiScan{ID: s.ID, FolderID: s.FolderID, RootPath: s.RootPath, CreatedAt: s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
	if s.CompletedAt != nil {
		t := s.CompletedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.CompletedAt = &t
	}
	if s.HashStartedAt != nil {
		t := s.HashStartedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.HashStartedAt = &t
	}
	if s.HashCompletedAt != nil {
		t := s.HashCompletedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.HashCompletedAt = &t
	}
	out.FileCount = s.FileCount
	out.ScanSkippedCount = s.ScanSkippedCount
	out.HashedFileCount = s.HashedFileCount
	out.HashedByteCount = s.HashedByteCount
	out.HashReusedCount = s.HashReusedCount
	out.HashErrorCount = s.HashErrorCount
	return out
}

type apiScanStatus struct {
	ID               int64   `json:"id"`
	CreatedAt        string  `json:"created_at"`
	CompletedAt      *string `json:"completed_at,omitempty"`
	HashStartedAt    *string `json:"hash_started_at,omitempty"`
	HashCompletedAt  *string `json:"hash_completed_at,omitempty"`
	FileCount        *int64  `json:"file_count,omitempty"`
	HashedFileCount  *int64  `json:"hashed_file_count,omitempty"`
	HashReusedCount  *int64  `json:"hash_reused_count,omitempty"`
	HashedReadCount  int64   `json:"hashed_read_count"` // files read from disk (hashed - reused)
	HashErrorCount   *int64  `json:"hash_error_count,omitempty"`
}

func scanStatusToAPI(s *db.Scan) apiScanStatus {
	out := apiScanStatus{ID: s.ID, CreatedAt: s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
	if s.CompletedAt != nil {
		t := s.CompletedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.CompletedAt = &t
	}
	if s.HashStartedAt != nil {
		t := s.HashStartedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.HashStartedAt = &t
	}
	if s.HashCompletedAt != nil {
		t := s.HashCompletedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.HashCompletedAt = &t
	}
	out.FileCount = s.FileCount
	out.HashedFileCount = s.HashedFileCount
	out.HashReusedCount = s.HashReusedCount
	out.HashedReadCount = s.HashedReadCount()
	out.HashErrorCount = s.HashErrorCount
	return out
}

// Duplicates API types.
type apiDuplicatesSummary struct {
	GroupCount      int64 `json:"group_count"`
	TotalFiles      int64 `json:"total_files"`
	TotalSize       int64 `json:"total_size"`
	ReclaimableSize int64 `json:"reclaimable_size"`
}

type apiDuplicateGroup struct {
	Hash            string     `json:"hash"`
	FileCount       int64      `json:"file_count"`
	TotalSize       int64      `json:"total_size"`
	ReclaimableSize int64      `json:"reclaimable_size"`
	Files           []apiFile  `json:"files,omitempty"`
}

type apiFile struct {
	ID       int64   `json:"id"`
	Path     string  `json:"path"`
	Size     int64   `json:"size"`
	FolderID int64   `json:"folder_id"`
	Hash     *string `json:"hash,omitempty"`
}

func fileToAPI(f *db.File) apiFile {
	out := apiFile{ID: f.ID, Path: f.Path, Size: f.Size, FolderID: f.FolderID}
	out.Hash = f.Hash
	return out
}

// Request body types.
type apiPostRootRequest struct {
	Path string `json:"path"`
}

type apiPostScanRequest struct {
	RootPath string `json:"root_path"`
	RootID   int64  `json:"root_id"`
}

// Handlers.

func (s *Server) apiHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
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

func (s *Server) apiRootsList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		roots, err := db.ListScanRoots(r.Context(), s.dbForRead())
		if err != nil {
			log.Printf("api list roots: %v", err)
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]apiRoot, len(roots))
		for i := range roots {
			out[i] = rootToAPI(&roots[i])
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) apiRootsCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body apiPostRootRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		path := trimSpace(body.Path)
		if path == "" {
			writeAPIError(w, http.StatusBadRequest, "path required")
			return
		}
		id, err := db.AddScanRoot(r.Context(), s.db, path)
		if err != nil {
			log.Printf("api add root: %v", err)
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		root, err := db.GetScanRoot(r.Context(), s.db, id)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Location", "/api/roots/"+strconv.FormatInt(id, 10))
		writeJSON(w, http.StatusCreated, rootToAPI(root))
	}
}

func (s *Server) apiRootsGet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id, err := parseID(r.PathValue("id"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid id")
			return
		}
		root, err := db.GetScanRoot(r.Context(), s.dbForRead(), id)
		if err != nil {
			if isNotFound(err) {
				writeAPIError(w, http.StatusNotFound, "root not found")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, rootToAPI(root))
	}
}

func (s *Server) apiScansList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		limit := 0
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		var scans []db.Scan
		var err error
		if limit > 0 {
			scans, err = db.ListScansRecent(r.Context(), s.dbForRead(), limit)
		} else {
			scans, err = db.ListScans(r.Context(), s.dbForRead())
		}
		if err != nil {
			log.Printf("api list scans: %v", err)
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]apiScan, len(scans))
		for i := range scans {
			out[i] = scanToAPI(&scans[i])
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) apiScansCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body apiPostScanRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		path := trimSpace(body.RootPath)
		var folderID int64
		if path == "" && body.RootID != 0 {
			root, err := db.GetScanRoot(r.Context(), s.db, body.RootID)
			if err != nil {
				if isNotFound(err) {
					writeAPIError(w, http.StatusNotFound, "root not found")
					return
				}
				writeAPIError(w, http.StatusInternalServerError, err.Error())
				return
			}
			path = root.Path
			folderID = root.ID
		}
		if path == "" {
			writeAPIError(w, http.StatusBadRequest, "root_path or root_id required")
			return
		}
		if folderID == 0 {
			var err error
			folderID, err = db.GetOrCreateFolderByPath(r.Context(), s.db, path)
			if err != nil {
				log.Printf("api create scan: %v", err)
				writeAPIError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		scanRow, err := db.CreateScan(r.Context(), s.db, folderID)
		if err != nil {
			log.Printf("api create scan: %v", err)
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		select {
		case s.scanQueue <- scanRow.ID:
		default:
			writeAPIError(w, http.StatusServiceUnavailable, "scan queue is full, try again later")
			return
		}
		w.Header().Set("Location", "/api/scans/"+strconv.FormatInt(scanRow.ID, 10))
		writeJSON(w, http.StatusAccepted, scanToAPI(scanRow))
	}
}

func (s *Server) apiScansGet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id, err := parseScanID(r.PathValue("id"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid id")
			return
		}
		sn, err := db.GetScan(r.Context(), s.dbForRead(), id)
		if err != nil {
			if isNotFound(err) {
				writeAPIError(w, http.StatusNotFound, "scan not found")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, scanToAPI(sn))
	}
}

func (s *Server) apiScansContinue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id, err := parseScanID(r.PathValue("id"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid id")
			return
		}
		sn, err := db.GetScan(r.Context(), s.dbForRead(), id)
		if err != nil {
			if isNotFound(err) {
				writeAPIError(w, http.StatusNotFound, "scan not found")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sn.CompletedAt != nil && sn.HashCompletedAt != nil {
			w.Header().Set("Location", "/api/scans/"+strconv.FormatInt(id, 10))
			writeJSON(w, http.StatusFound, scanToAPI(sn))
			return
		}
		if err := db.ResetHashStatusHashingToPending(r.Context(), s.db, id); err != nil {
			log.Printf("api scan continue: %v", err)
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		select {
		case s.scanQueue <- id:
		default:
			writeAPIError(w, http.StatusServiceUnavailable, "scan queue is full, try again later")
			return
		}
		w.Header().Set("Location", "/api/scans/"+strconv.FormatInt(id, 10))
		writeJSON(w, http.StatusAccepted, scanToAPI(sn))
	}
}

func (s *Server) apiScansStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id, err := parseScanID(r.PathValue("id"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid id")
			return
		}
		sn, err := db.GetScan(r.Context(), s.dbForRead(), id)
		if err != nil {
			if isNotFound(err) {
				writeAPIError(w, http.StatusNotFound, "scan not found")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, scanStatusToAPI(sn))
	}
}

func (s *Server) apiDuplicatesSummary() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		summary, err := db.GetDuplicateGroupsHashSummary(r.Context(), s.dbForRead())
		if err != nil {
			log.Printf("api duplicates summary: %v", err)
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, apiDuplicatesSummary{
			GroupCount:      summary.GroupCount,
			TotalFiles:      summary.TotalFiles,
			TotalSize:       summary.TotalSize,
			ReclaimableSize: summary.ReclaimableSize,
		})
	}
}

const defaultDuplicatesGroupsLimit = 20
const defaultMaxFilesPerGroup = 10

func (s *Server) apiDuplicatesGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		limit := defaultDuplicatesGroupsLimit
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		offset := 0
		if o := r.URL.Query().Get("offset"); o != "" {
			if n, err := strconv.Atoi(o); err == nil && n >= 0 {
				offset = n
			}
		}
		maxFiles := defaultMaxFilesPerGroup
		if m := r.URL.Query().Get("max_files_per_group"); m != "" {
			if n, err := strconv.Atoi(m); err == nil && n >= 0 {
				maxFiles = n
			}
		}
		total, err := db.DuplicateGroupsHashCount(r.Context(), s.dbForRead())
		if err != nil {
			log.Printf("api duplicates groups count: %v", err)
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		groups, err := db.DuplicateGroupsHashPaginated(r.Context(), s.dbForRead(), limit, offset)
		if err != nil {
			log.Printf("api duplicates groups: %v", err)
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		apiGroups := make([]apiDuplicateGroup, len(groups))
		for i := range groups {
			g := &groups[i]
			reclaimable := int64(0)
			if g.Count > 0 {
				reclaimable = g.Size - g.Size/g.Count
			}
			ag := apiDuplicateGroup{Hash: g.Hash, FileCount: g.Count, TotalSize: g.Size, ReclaimableSize: reclaimable}
			files, _ := db.FilesInHashGroupByHash(r.Context(), s.dbForRead(), g.Hash, maxFiles)
			ag.Files = make([]apiFile, len(files))
			for j := range files {
				ag.Files[j] = fileToAPI(&files[j])
			}
			apiGroups[i] = ag
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"groups": apiGroups, "total": total})
	}
}

// Helpers used by API.
func parseID(idStr string) (int64, error) {
	return strconv.ParseInt(idStr, 10, 64)
}

func trimSpace(s string) string {
	return strings.TrimSpace(s)
}

func isNotFound(err error) bool {
	return err != nil && (errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows"))
}
