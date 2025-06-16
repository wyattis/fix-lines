package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/wlynxg/chardet"
	"github.com/wyattis/z/zset/zstringset"
)

// TODO: Handle other encodings besides UTF-8 and ASCII
// TODO: Respect .ignore files

var log = slog.Default()

func main() {
	if err := run(); err != nil {
		log.Error("error", "error", err)
		os.Exit(1)
	}
}

var dryRun = flag.Bool("dry-run", false, "don't actually write any files")
var verbose = flag.Bool("verbose", false, "verbose logging")
var probeSize = flag.Int("probe-size", 1024, "how much of each file to probe for encoding")
var help = flag.Bool("help", false, "show help")

func run() error {
	flag.Parse()
	if *help {
		flag.Usage()
		return nil
	}
	if *verbose {
		log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	}
	roots := flag.Args()
	if len(roots) == 0 {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		roots = []string{wd}
	}
	paths, err := expandPatterns(roots)
	if err != nil {
		return err
	}

	for _, path := range paths {
		if err := handlePath(path); err != nil {
			return err
		}
	}

	return nil
}

func handlePath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return handleDir(path)
	}
	return handleFile(path)
}

func handleDir(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		return handleFile(path)
	})
}

func handleFile(path string) error {
	isText, encoding, err := isTextFile(path)
	if err != nil {
		return err
	}
	if !isText {
		return nil
	}
	if !supportedEncodings.Contains(strings.ToUpper(encoding)) {
		slog.Info("skipping unsupported encoding", "path", path, "encoding", encoding)
		return nil
	}
	return replaceLines(path, encoding)
}

func safeFileRewrite(path string, cb func(input, output *os.File) error) (err error) {
	tmpPath := fmt.Sprintf("%s.tmp", path)
	log.Debug("creating temporary file", "path", tmpPath)
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return
	}
	isTmpClosed := false
	defer func() {
		if !isTmpClosed {
			tmpFile.Close()
		}
	}()
	input, err := os.Open(path)
	if err != nil {
		return
	}
	defer input.Close()
	isInputClosed := false
	defer func() {
		if !isInputClosed {
			input.Close()
		}
	}()
	if err = cb(input, tmpFile); err != nil {
		return
	}
	log.Debug("closing temporary file", "path", tmpPath)
	if err = tmpFile.Close(); err != nil {
		return
	}
	isTmpClosed = true
	if err = input.Close(); err != nil {
		return
	}
	isInputClosed = true
	log.Debug("renaming temporary file", "path", tmpPath, "to", path)
	return os.Rename(tmpPath, path)
}

func replaceLines(path string, encoding string) error {
	switch strings.ToUpper(encoding) {
	case "UTF-8", "ASCII":
		log.Info("replacing lines", "path", path, "encoding", encoding)
		if *dryRun {
			return nil
		}
		return safeFileRewrite(path, replaceUtf8)
	default:
		return fmt.Errorf("unsupported encoding: %s", encoding)
	}
}

func replaceUtf8(input *os.File, output *os.File) error {
	buf := bufio.NewReader(input)
	outBuf := bufio.NewWriter(output)
	scanner := bufio.NewScanner(buf)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		if scanner.Err() != nil {
			return scanner.Err()
		}
		line := scanner.Text()
		log.Debug("replacing line", "line", line)
		outBuf.WriteString(line + "\n")
	}
	if scanner.Err() != nil {
		return scanner.Err()
	}
	return outBuf.Flush()
}

func expandPatterns(patterns []string) ([]string, error) {
	var paths []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		paths = append(paths, matches...)
	}
	return paths, nil
}

var supportedEncodings = zstringset.New("UTF-8", "ASCII")
var detector = chardet.NewUniversalDetector(0)

func isTextFile(path string) (isText bool, encoding string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	log.Debug("checking if file is text", "path", path)
	return isTextFileReader(file)
}

func isTextFileReader(file io.Reader) (isText bool, encoding string, err error) {
	detector.Reset()
	var maxChunks = 20
	var chunk = make([]byte, *probeSize)
	var requiredConfidence = 0.95
	for i := 0; i < maxChunks; i++ {
		log.Debug("reading chunk", "chunk", i)
		n, err := file.Read(chunk)
		log.Debug("read chunk", "chunk", i, "n", n, "err", err)
		if err == io.EOF {
			if n == 0 {
				break
			}
			log.Debug("EOF w/ data read")
			err = nil
		}
		if err != nil {
			return false, "", err
		}
		detector.Feed(chunk[:n])
		result := detector.GetResult()
		if result.Confidence > requiredConfidence {
			return true, result.Encoding, nil
		}
	}
	return false, "", nil
}
