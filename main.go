/*
	Andrew Pfaendler

	resize videos -> 30fps amd streamable bitrate <= 5M b/s

*/

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Args struct {
	Inpath  string
	Outpath string
	Diff    bool
	Recurse bool
}

var TYPES = []string{"mp4"}

func main() {
	var (
		outPathF   = flag.String("o", "out_vids", "out path")
		diffFilesF = flag.Bool("diff", false, "only process files not in out_vids")
		recurseF   = flag.Bool("r", false, "recursive process")
	)
	flag.Parse()

	if flag.NArg() < 1 {
		log.Fatal("expected path to process: $ resize_vids my_vids")
	}

	args := Args{
		Inpath:  flag.Args()[0],
		Outpath: *outPathF,
		Diff:    *diffFilesF,
		Recurse: *recurseF,
	}

	// remove existing dir if exists and not in differential mode
	if !args.Diff {
		if err := remove_out_dir(args.Outpath); err != nil {
			log.Fatal(err)
		}

		// make output folder
		// fmt.Printf("making out dir: %s\n", args.Outpath)
		// if err := os.MkdirAll(*outPathF, 0755); err != nil {
		// 	log.Fatal(err)
		// }
	}

	process_files(args.Inpath, args.Outpath, args)
	fmt.Println("done")

}

func remove_out_dir(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err == nil {
		fmt.Printf("removing current outpath dir: %s\n", path)
		os.RemoveAll(path)
		return nil
	} else {
		return err
	}
}

func isFile(path string) bool {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false // Handle errors like file not found
	}
	return !fileInfo.IsDir()
}

// does the file have an extension we can handle
func canProc(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" {
		return slices.Contains(TYPES, ext[1:])
	}
	return false
}

func process_files(inpath string, outpath string, args Args) {
	var existing_files = []os.DirEntry{}
	if args.Diff {
		var err error
		existing_files, err = os.ReadDir(outpath)
		if err != nil {
			log.Fatalf("error processing exisitng output files\n%s\n", err)
		}
	}

	// single file case (no diffing)
	if isFile(inpath) {
		fmt.Printf("%s processing as single file\n", inpath)
		if !canProc(inpath) {
			fmt.Printf("%s is not a mp4 file\n", inpath)
			return
		}

		ffm := New_FFMpeg(inpath, outpath)
		err := ffm.process_file()
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	// flat file case (add recursion later)
	files, err := os.ReadDir(inpath)
	if err != nil {
		log.Fatalf("error processing directory files\n%s\n", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if args.Diff && contains_file(existing_files, file) {
			log.Printf("skipping: %s (already exists)\n", file.Name())
			continue
		}

		file_path := filepath.Join(inpath, file.Name())
		if !canProc(file.Name()) {
			log.Printf("skipping: %s (not mp4)\n", file_path)
			continue
		}

		ffm := New_FFMpeg(file_path, outpath)
		err := ffm.process_file()
		if err != nil {
			log.Fatal(err)
		}

	}

	// recursive case
	if args.Recurse {
		for _, file := range files {
			if !file.IsDir() {
				continue
			}

			next_src_dir := filepath.Join(inpath, file.Name())
			next_out_dir := filepath.Join(outpath, file.Name())
			process_files(next_src_dir, next_out_dir, args)
		}

	}

}

// check if file is in existing set of files
func contains_file(existing []os.DirEntry, current os.DirEntry) bool {
	for _, entry := range existing {
		if entry.Name() == current.Name() && entry.IsDir() == current.IsDir() {
			return true
		}
	}
	return false
}
