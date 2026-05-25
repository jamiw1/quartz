package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/google/uuid"
)

var safeExtRegex = regexp.MustCompile(`^\.[a-zA-Z0-9]{1,10}$`)

func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\\' || r == '\r' || r == '\n' {
			return '_'
		}
		return r
	}, s)
}

func main() {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "."
	}

	dbPath := filepath.Join(dataDir, "quartz.db")
	uploadsDir := filepath.Join(dataDir, "uploads")

	InitDB(dbPath)
	defer CloseDB()

	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		log.Fatal("failed to create uploads directory:", err)
	}

	app := fiber.New(fiber.Config{
		BodyLimit:   150 * 1024 * 1024,
		TrustProxy:  true,
		ProxyHeader: "X-Forwarded-For",
	})

	app.Use(limiter.New(limiter.Config{
		Max:        100,
		Expiration: 1 * time.Minute,
	}))

	app.Use("/", static.New("./public"))

	app.Use("/upload", limiter.New(limiter.Config{
		Max:        3,
		Expiration: 1 * time.Minute,
	}))

	app.Post("/upload", func(c fiber.Ctx) error {
		file, err := c.FormFile("document")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing file payload"})
		}

		linkCount := 1
		if lcStr := c.FormValue("link_count"); lcStr != "" {
			var val int
			if _, err := fmt.Sscanf(lcStr, "%d", &val); err == nil && val >= 1 && val <= 100 {
				linkCount = val
			}
		}

		fileID := uuid.New().String()

		extension := filepath.Ext(file.Filename)
		if !safeExtRegex.MatchString(extension) {
			extension = ""
		}

		diskName := fileID + extension
		storagePath := filepath.Join(uploadsDir, diskName)

		uploadsAbs, _ := filepath.Abs(uploadsDir)
		storageAbs, _ := filepath.Abs(storagePath)
		if !strings.HasPrefix(storageAbs, uploadsAbs+string(filepath.Separator)) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid file path"})
		}

		if err := c.SaveFile(file, storagePath); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to write file"})
		}

		linkIDs := make([]string, linkCount)
		for i := 0; i < linkCount; i++ {
			linkIDs[i] = uuid.New().String()
		}

		expiryTime := time.Now().Add(5 * 24 * time.Hour).Unix()
		if err := SaveFileWithLinks(fileID, file.Filename, storagePath, expiryTime, linkIDs); err != nil {
			_ = os.Remove(storagePath)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to log file meta"})
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"status": "success",
			"links":  linkIDs,
		})
	})

	app.Get("/download/:id", func(c fiber.Ctx) error {
		fileID := c.Params("id")

		userAgent := c.Get("User-Agent")
		isBot := strings.Contains(strings.ToLower(userAgent), "discordbot") ||
			strings.Contains(strings.ToLower(userAgent), "telegrambot") ||
			strings.Contains(strings.ToLower(userAgent), "slackbot")

		if isBot {
			f, err := GetFileByLink(fileID)
			if err == sql.ErrNoRows {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found"})
			} else if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database lookup failed"})
			}

			if time.Now().Unix() > f.Expiry {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found"})
			}

			c.Set("Content-Type", "text/html")
			safeName := sanitizeFilename(f.OriginalName)
			return c.SendString(fmt.Sprintf(`<!DOCTYPE html>
				<html>
				<head>
					<meta charset="utf-8">
					<title>%s</title>
					<meta property="og:title" content="%s" />
					<meta property="og:description" content="quartz - careful, this is a one-time download link!" />
					<meta name="twitter:card" content="summary" />
				</head>
				<body>
					<p>file: %s</p>
				</body>
				</html>`, safeName, safeName, safeName))
		}

		f, isLastLink, err := ClaimLink(fileID)
		if err == sql.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found"})
		} else if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database lookup failed"})
		}

		if time.Now().Unix() > f.Expiry {
			if isLastLink {
				_ = os.Remove(f.Path)
			}
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found"})
		}

		diskFile, err := os.Open(f.Path)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to read file data"})
		}

		safeName := sanitizeFilename(f.OriginalName)

		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeName))
		c.Set("Content-Type", "application/octet-stream")

		return c.SendStreamWriter(func(w *bufio.Writer) {
			defer func() {
				_ = diskFile.Close()
				if isLastLink {
					_ = os.Remove(f.Path)
				}
			}()
			if _, err := io.Copy(w, diskFile); err != nil {
				log.Printf("warning: error streaming file %s: %v", f.Path, err)
			}
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	startCleanupTimer(ctx)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Printf("quartz listening on :%s", port)
	go func() {
		if err := app.Listen(":"+port, fiber.ListenConfig{DisableStartupMessage: true}); err != nil {
			log.Printf("server stopped: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	cancel()
	if err := app.ShutdownWithTimeout(5 * time.Second); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

func CleanupExpiredFiles() {
	now := time.Now().Unix()
	query := `SELECT id, path FROM files WHERE expiry < ?;`

	rows, err := db.Query(query, now)
	if err != nil {
		log.Printf("error: failed to query expired files from database: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			log.Printf("error: failed to scan expired file row: %v", err)
			continue
		}

		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("error: failed to remove expired file %s from disk: %v", path, err)
		}

		if err := DeleteFile(id); err != nil {
			log.Printf("error: failed to delete expired file metadata %s from db: %v", id, err)
		}
	}
}

func startCleanupTimer(ctx context.Context) {
	CleanupExpiredFiles()

	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				CleanupExpiredFiles()
			case <-ctx.Done():
				return
			}
		}
	}()
}