package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/eargollo/ditto/internal/db"
	"github.com/eargollo/ditto/internal/hash"
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

func (s *Server) apiAdminRefreshDuplicateGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		ctx := r.Context()
		var err error
		if dbConn, ok := s.db.(*sql.DB); ok {
			err = db.PrecomputeDuplicateGroupsHashInTransaction(ctx, dbConn)
		} else {
			err = db.PrecomputeDuplicateGroupsHash(ctx, s.db)
		}
		if err != nil {
			log.Printf("api admin refresh duplicate groups: %v", err)
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// apiPostGroupRefreshRequest is the body for POST /api/duplicates/groups/refresh.
type apiPostGroupRefreshRequest struct {
	Hash string `json:"hash"`
}

// runGroupRefreshForHash updates deleted_at and hash_status for files in the group by Stat'ing each path, then refreshes duplicate_groups_hash.
// Paths come from DB only (FilesForGroupRefresh). Used by both group-refresh and delete handlers.
func runGroupRefreshForHash(ctx context.Context, q db.Querier, groupHash string) error {
	files, err := db.FilesForGroupRefresh(ctx, q, groupHash)
	if err != nil {
		return err
	}
	for _, f := range files {
		// f.Path is from DB: folder.path || '/' || file.path (our scanner), not user input.
		// #nosec G304 G703 -- path is server-controlled, not from request
		info, err := os.Stat(f.Path)
		if err != nil {
			if os.IsNotExist(err) {
				if err := db.SetFileDeletedAt(ctx, q, f.ID); err != nil {
					log.Printf("group refresh: set deleted_at for file %d: %v", f.ID, err)
				}
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		needReset := info.Size() != f.Size
		if !needReset && f.HashedMtime != nil {
			needReset = info.ModTime().Unix() != *f.HashedMtime
		}
		if needReset {
			if err := db.SetFileHashReset(ctx, q, f.ID); err != nil {
				log.Printf("group refresh: set hash reset for file %d: %v", f.ID, err)
			}
		}
	}
	return db.RefreshDuplicateGroupsForHashes(ctx, q, []string{groupHash})
}

func (s *Server) apiDuplicatesGroupRefresh() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body apiPostGroupRefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		groupHash := trimSpace(body.Hash)
		if groupHash == "" {
			writeAPIError(w, http.StatusBadRequest, "hash required")
			return
		}
		ctx := r.Context()
		if err := runGroupRefreshForHash(ctx, s.db, groupHash); err != nil {
			log.Printf("api duplicates group refresh: %v", err)
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// apiPostDeleteFileRequest is the body for POST /api/duplicates/files/delete. Only file_id is accepted; paths/hashes come from DB only.
type apiPostDeleteFileRequest struct {
	FileID int64 `json:"file_id"`
}

// writeStreamError sends a plain-text error line for streaming delete (stream=1). No flush needed.
func writeStreamError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	// Single line so client can parse; newlines in msg replaced with space.
	_, err := w.Write([]byte("ERROR: " + strings.ReplaceAll(msg, "\n", " ") + "\n")) // #nosec G705 -- response is text/plain; msg is server error
	if err != nil {
		log.Printf("api writeStreamError: write failed: %v", err)
	}
}

func abbrevHash(h string) string {
	if len(h) <= 7 {
		return h
	}
	return h[:7]
}

func (s *Server) apiDuplicatesFileDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body apiPostDeleteFileRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		fileID := body.FileID
		if fileID <= 0 {
			log.Printf("api delete file: attempt file_id=%d rejected: invalid file_id", fileID)
			writeAPIError(w, http.StatusBadRequest, "file_id required and must be positive")
			return
		}
		log.Printf("api delete file: attempt file_id=%d", fileID)
		ctx := r.Context()
		stream := r.URL.Query().Get("stream") == "1"

		// Load file from DB only; never use client-supplied path/hash.
		file, err := db.GetFileByID(ctx, s.db, fileID)
		if err != nil {
			log.Printf("api delete file: file_id=%d GetFileByID error: %v", fileID, err)
			if stream {
				writeStreamError(w, http.StatusInternalServerError, err.Error())
			} else {
				writeAPIError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		if file == nil {
			log.Printf("api delete file: file_id=%d rejected: file not found or already deleted", fileID)
			if stream {
				writeStreamError(w, http.StatusBadRequest, "file not found or already deleted")
			} else {
				writeAPIError(w, http.StatusBadRequest, "file not found or already deleted")
			}
			return
		}
		if file.Hash == nil || *file.Hash == "" {
			log.Printf("api delete file: file_id=%d rejected: file has no hash", fileID)
			if stream {
				writeStreamError(w, http.StatusBadRequest, "file has no hash; cannot delete")
			} else {
				writeAPIError(w, http.StatusBadRequest, "file has no hash; cannot delete")
			}
			return
		}
		groupHash := *file.Hash

		// Guard: at least one file must remain. Count only files that exist on disk and have unchanged size/mtime.
		refreshFiles, err := db.FilesForGroupRefresh(ctx, s.db, groupHash)
		if err != nil {
			log.Printf("api delete file: FilesForGroupRefresh: %v", err)
			if stream {
				writeStreamError(w, http.StatusInternalServerError, err.Error())
			} else {
				writeAPIError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		validCount := 0
		for _, f := range refreshFiles {
			// #nosec G304 G703 -- path from DB only
			info, err := os.Stat(f.Path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if info.Size() != f.Size {
				continue
			}
			if f.HashedMtime != nil && info.ModTime().Unix() != *f.HashedMtime {
				continue
			}
			validCount++
		}
		if validCount <= 1 {
			msg := "cannot delete: at least one file must remain in the group (some files may be missing or changed; try refreshing the group)"
			log.Printf("api delete file: file_id=%d rejected: at least one file must remain in group (valid count=%d)", fileID, validCount)
			if stream {
				writeStreamError(w, http.StatusBadRequest, msg)
			} else {
				writeAPIError(w, http.StatusBadRequest, msg)
			}
			return
		}

		// Guard: re-hash file to delete and one other file; both must match stored hashes.
		groupFiles, err := db.FilesInHashGroupByHash(ctx, s.db, groupHash, 0)
		if err != nil {
			log.Printf("api delete file: FilesInHashGroupByHash: %v", err)
			if stream {
				writeStreamError(w, http.StatusInternalServerError, err.Error())
			} else {
				writeAPIError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		var other *db.File
		for i := range groupFiles {
			if groupFiles[i].ID != fileID {
				other = &groupFiles[i]
				break
			}
		}
		if other == nil || other.Hash == nil {
			log.Printf("api delete file: file_id=%d rejected: no other file in group to verify", fileID)
			if stream {
				writeStreamError(w, http.StatusBadRequest, "no other file in group to verify")
			} else {
				writeAPIError(w, http.StatusBadRequest, "no other file in group to verify")
			}
			return
		}

		// Optional stream writer: when stream=1, write progress lines and flush after each.
		// Lines are server-constructed from DB paths; response is text/plain (G705).
		var writeLine func(string)
		if stream {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			fl, _ := w.(http.Flusher)
			writeLine = func(line string) {
				if _, err := w.Write([]byte(line + "\n")); err != nil { // #nosec G705 -- response is text/plain; line is server-constructed from DB
					log.Printf("api delete file: stream write failed: %v", err)
					return
				}
				if fl != nil {
					fl.Flush()
				}
			}
		} else {
			writeLine = func(string) {}
		}

		writeLine("Starting file check")

		writeLine("Hashing file " + file.Path)
		computedToDelete, err := hash.HashFile(file.Path)
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("api delete file: file_id=%d rejected: file not found on disk path=%s", fileID, file.Path)
				writeLine("ERROR: file not found on disk")
				return
			}
			log.Printf("api delete file: file_id=%d hash error: %v", fileID, err)
			writeLine("ERROR: " + err.Error())
			return
		}
		if computedToDelete != *file.Hash {
			log.Printf("api delete file: file_id=%d rejected: file content has changed", fileID)
			writeLine("ERROR: file content has changed; refresh the group and try again")
			return
		}
		writeLine("File " + file.Path + " hashed. Hash value " + abbrevHash(computedToDelete))

		writeLine("Hashing file " + other.Path)
		computedOther, err := hash.HashFile(other.Path)
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("api delete file: file_id=%d rejected: other file in group missing on disk", fileID)
				writeLine("ERROR: another file in the group is missing on disk; refresh the group and try again")
				return
			}
			log.Printf("api delete file: file_id=%d hash other error: %v", fileID, err)
			writeLine("ERROR: " + err.Error())
			return
		}
		if computedOther != *other.Hash {
			log.Printf("api delete file: file_id=%d rejected: file or group content has changed", fileID)
			writeLine("ERROR: file or group content has changed; refresh the group and try again")
			return
		}
		writeLine("File " + other.Path + " hashed. Hash value " + abbrevHash(computedOther))

		writeLine("Deleting file " + file.Path)
		// #nosec G304 G703 -- path from DB only
		if err := os.Remove(file.Path); err != nil {
			if os.IsNotExist(err) {
				log.Printf("api delete file: file_id=%d path=%s: file already missing on disk, marking deleted in DB", fileID, file.Path)
			} else {
				log.Printf("api delete file: file_id=%d os.Remove failed: %v", fileID, err)
				writeLine("ERROR: " + err.Error())
				return
			}
		}
		writeLine("File " + file.Path + " deleted")

		// Use a context that is not canceled when the client disconnects (e.g. after a long hash).
		// We must complete DB updates so the file is marked deleted and the group refreshed.
		detachedCtx := context.WithoutCancel(ctx)

		writeLine("Updating database (marking file as deleted)")
		if err := db.SetFileDeletedAt(detachedCtx, s.db, fileID); err != nil {
			log.Printf("api delete file: file_id=%d SetFileDeletedAt: %v", fileID, err)
			writeLine("ERROR: " + err.Error())
			return
		}
		writeLine("Database updated")

		writeLine("Refreshing duplicate group")
		if err := runGroupRefreshForHash(detachedCtx, s.db, groupHash); err != nil {
			log.Printf("api delete file: file_id=%d runGroupRefreshForHash: %v", fileID, err)
			writeLine("ERROR: " + err.Error())
			return
		}
		writeLine("Group refreshed")

		log.Printf("api delete file: deleted file_id=%d path=%s", fileID, file.Path)
		writeLine("DONE")
		if !stream {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
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
