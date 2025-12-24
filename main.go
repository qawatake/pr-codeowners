package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: pr-codeowners <PR番号 or URL>")
		os.Exit(1)
	}

	prRef := os.Args[1]

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	logf := func(format string, args ...interface{}) {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	}

	logf("データを並列取得中: %s", prRef)

	// Fetch all data in parallel (PR info, files, CODEOWNERS)
	info, files, codeowners, err := FetchPRData(ctx, prRef)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	logf("取得完了 - リポジトリ: %s/%s, ファイル数: %d", info.Owner, info.Repo, len(files))

	// Parse CODEOWNERS and match files
	matcher := ParseCodeowners(codeowners)
	result := BuildOwnerFilesMap(matcher, files)

	// Output JSON
	jsonOutput, err := ToJSON(result)
	if err != nil {
		log.Fatalf("Error creating JSON: %v", err)
	}

	logf("完了！ (%.2fs)", time.Since(start).Seconds())
	fmt.Println(jsonOutput)
}
