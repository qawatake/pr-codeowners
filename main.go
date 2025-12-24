package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/qawatake/pr-codeowners/internal/codeowners"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func main() {
	verbose := flag.Bool("v", false, "verbose output")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: pr-codeowners [-v] <PR番号 or URL>")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	prRef := flag.Arg(0)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	logf := func(format string, args ...interface{}) {
		if *verbose {
			fmt.Fprintf(os.Stderr, "[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
		}
	}

	// Start spinner
	stopSpinner := make(chan struct{})
	if !*verbose {
		go func() {
			i := 0
			for {
				select {
				case <-stopSpinner:
					fmt.Fprintf(os.Stderr, "\r\033[K")
					return
				default:
					fmt.Fprintf(os.Stderr, "\r%s Fetching PR data...", spinnerFrames[i%len(spinnerFrames)])
					i++
					time.Sleep(80 * time.Millisecond)
				}
			}
		}()
	}

	logf("データを並列取得中: %s", prRef)

	// Fetch all data in parallel (PR info, files, CODEOWNERS, reviewers)
	info, files, codeownersContent, reviewers, err := codeowners.FetchPRDataWithReviewers(ctx, prRef)
	if err != nil {
		close(stopSpinner)
		log.Fatalf("Error: %v", err)
	}

	logf("取得完了 - リポジトリ: %s/%s, ファイル数: %d, レビュアー数: %d", info.Owner, info.Repo, len(files), len(reviewers))

	// Parse CODEOWNERS and match files with reviewers
	matcher := codeowners.ParseCodeowners(codeownersContent)
	result := codeowners.BuildOwnerFilesMapWithReviewers(ctx, matcher, files, reviewers)

	// Stop spinner
	close(stopSpinner)
	time.Sleep(100 * time.Millisecond) // Wait for spinner cleanup

	// Output JSON
	jsonOutput, err := codeowners.ToJSON(result)
	if err != nil {
		log.Fatalf("Error creating JSON: %v", err)
	}

	logf("完了！ (%.2fs)", time.Since(start).Seconds())
	fmt.Println(jsonOutput)
}
