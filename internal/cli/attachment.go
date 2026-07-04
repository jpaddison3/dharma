package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jpaddison3/dharma/internal/client"
	"github.com/jpaddison3/dharma/internal/output"
	"github.com/spf13/cobra"
)

var attachmentCmd = &cobra.Command{
	Use:   "attachment",
	Short: "Attachment commands",
}

var (
	attachmentDownloadOutput    string
	attachmentDownloadOutputDir string
)

var attachmentDownloadCmd = &cobra.Command{
	Use:   "download <gid>",
	Short: "Download an attachment to disk",
	Long: `Download an attachment to disk. Pass exactly one of --output (exact path) or
--output-dir (uses the attachment's name as the filename, sanitized).

On success, prints one JSON line to stdout: {"attachment_gid","path","bytes"}.
On signed-URL expiry (HTTP 401/403 from the storage host) the metadata is
re-fetched once and the download retried.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if (attachmentDownloadOutput == "") == (attachmentDownloadOutputDir == "") {
			return usageErrorf("pass exactly one of --output or --output-dir")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		result, err := downloadAttachment(context.Background(), c, args[0], attachmentDownloadOutput, attachmentDownloadOutputDir)
		if err != nil {
			return err
		}
		return output.Print(os.Stdout, result)
	},
}

var taskDownloadAttachmentsDir string

var taskDownloadAttachmentsCmd = &cobra.Command{
	Use:   "download-attachments <task-gid>",
	Short: "Download all attachments on a task into a directory",
	Long: `List attachments on the task and download each into --output-dir, using the
attachment's name (sanitized) as the filename. Prints one JSON line per file
to stdout. Continues on per-attachment errors and reports them to stderr; exits
non-zero if any download failed.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskDownloadAttachmentsDir == "" {
			return usageErrorf("--output-dir is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx := context.Background()
		q := url.Values{}
		q.Set("limit", "100")
		q.Set("opt_fields", "name")
		var attachments []struct {
			GID  string `json:"gid"`
			Name string `json:"name"`
		}
		resp, err := c.Do(ctx, "GET", "/tasks/"+args[0]+"/attachments", q, nil)
		if err != nil {
			return fmt.Errorf("list attachments: %w", err)
		}
		if err := json.Unmarshal(resp.Data, &attachments); err != nil {
			return fmt.Errorf("decode attachment list: %w", err)
		}
		if len(attachments) == 0 {
			fmt.Fprintln(os.Stderr, "no attachments on task")
			return nil
		}
		nameCounts := map[string]int{}
		for _, a := range attachments {
			nameCounts[sanitizeFilename(a.Name)]++
		}
		var failed int
		for _, a := range attachments {
			outputPath := ""
			if nameCounts[sanitizeFilename(a.Name)] > 1 {
				outputPath = filepath.Join(taskDownloadAttachmentsDir, disambiguateFilename(a.Name, a.GID))
			}
			result, err := downloadAttachment(ctx, c, a.GID, outputPath, taskDownloadAttachmentsDir)
			if err != nil {
				failed++
				fmt.Fprintf(os.Stderr, "attachment %s (%s): %v\n", a.GID, a.Name, err)
				continue
			}
			if err := output.Print(os.Stdout, result); err != nil {
				return err
			}
		}
		if failed > 0 {
			return fmt.Errorf("%d of %d attachments failed", failed, len(attachments))
		}
		return nil
	},
}

type downloadResult struct {
	AttachmentGID string `json:"attachment_gid"`
	Path          string `json:"path"`
	Bytes         int64  `json:"bytes"`
}

type attachmentMetadata struct {
	GID         string `json:"gid"`
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
}

func fetchAttachmentMetadata(ctx context.Context, c *client.Client, gid string) (*attachmentMetadata, error) {
	q := url.Values{}
	q.Set("opt_fields", "name,download_url")
	var meta attachmentMetadata
	if err := c.Get(ctx, "/attachments/"+gid, q, &meta); err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}
	if meta.DownloadURL == "" {
		return nil, fmt.Errorf("metadata: download_url empty (attachment may not be downloadable)")
	}
	return &meta, nil
}

func downloadAttachment(ctx context.Context, c *client.Client, gid, outputPath, outputDir string) (*downloadResult, error) {
	meta, err := fetchAttachmentMetadata(ctx, c, gid)
	if err != nil {
		return nil, err
	}

	destPath := outputPath
	if destPath == "" {
		filename := sanitizeFilename(meta.Name)
		if filename == "" {
			filename = gid
		}
		destPath = filepath.Join(outputDir, filename)
	}
	if dir := filepath.Dir(destPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("write: mkdir %s: %w", dir, err)
		}
	}

	bytes, err := fetchURLToFile(ctx, meta.DownloadURL, destPath)
	if err != nil {
		var sigErr *signedURLAuthError
		if errors.As(err, &sigErr) {
			meta, err = fetchAttachmentMetadata(ctx, c, gid)
			if err != nil {
				return nil, err
			}
			bytes, err = fetchURLToFile(ctx, meta.DownloadURL, destPath)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return &downloadResult{AttachmentGID: gid, Path: destPath, Bytes: bytes}, nil
}

type signedURLAuthError struct {
	StatusCode int
}

func (e *signedURLAuthError) Error() string {
	return fmt.Sprintf("download: signed URL returned HTTP %d (signature likely expired)", e.StatusCode)
}

func fetchURLToFile(ctx context.Context, downloadURL, destPath string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return 0, fmt.Errorf("download: build request: %w", err)
	}
	// Intentionally no Authorization header — the signed URL is on a different host
	// (typically S3/CloudFront) and forwarding the Asana PAT would leak it.
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return 0, &signedURLAuthError{StatusCode: resp.StatusCode}
	}
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return 0, fmt.Errorf("write: %w", err)
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return n, fmt.Errorf("write: %w", err)
	}
	return n, nil
}

// disambiguateFilename inserts the attachment gid before the extension so two
// attachments with identical names (common with screenshots called "image.png")
// don't clobber each other.
func disambiguateFilename(name, gid string) string {
	cleaned := sanitizeFilename(name)
	if cleaned == "" {
		return gid
	}
	ext := filepath.Ext(cleaned)
	stem := strings.TrimSuffix(cleaned, ext)
	return stem + "-" + gid + ext
}

var unsafeFilenameChars = regexp.MustCompile(`[\x00-\x1f/\\:*?"<>|]`)

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	cleaned := unsafeFilenameChars.ReplaceAllString(name, "_")
	cleaned = strings.Trim(cleaned, " .")
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return ""
	}
	return cleaned
}

func init() {
	attachmentDownloadCmd.Flags().StringVar(&attachmentDownloadOutput, "output", "", "exact output path (creates parent dirs)")
	attachmentDownloadCmd.Flags().StringVar(&attachmentDownloadOutputDir, "output-dir", "", "directory; filename comes from attachment name (sanitized)")
	attachmentCmd.AddCommand(attachmentDownloadCmd)

	taskDownloadAttachmentsCmd.Flags().StringVar(&taskDownloadAttachmentsDir, "output-dir", "", "destination directory (required)")
	taskCmd.AddCommand(taskDownloadAttachmentsCmd)
}
