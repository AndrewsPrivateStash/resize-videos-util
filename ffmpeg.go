/*
	FFMPEG and FFPROBE objects

*/

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type FFProbeResult struct {
	Format struct {
		Filename   string `json:"filename"`
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"` // Duration in seconds as a string
		Size       string `json:"size"`     // Size in bytes as a string
		BitRate    string `json:"bit_rate"` // Bitrate as a string
	} `json:"format"`
	Streams []struct {
		Index        int    `json:"index"`
		CodecName    string `json:"codec_name"`
		CodecType    string `json:"codec_type"`
		Width        int    `json:"width,omitempty"`  // Only for video streams
		Height       int    `json:"height,omitempty"` // Only for video streams
		AvgFrameRate string `json:"avg_frame_rate,omitempty"`
		NBFrames     string `json:"nb_frames"`
		SampleRate   string `json:"sample_rate,omitempty"` // Only for audio streams
		Channels     int    `json:"channels,omitempty"`    // Only for audio streams
	} `json:"streams"`
}

type Options struct {
	Proc      bool   `json:"proc"`      // process video otherwise copy
	Bitrate   string `json:"bitrate"`   // bitrate target {500, 400k, 5M, etc}
	Framerate int    `json:"framerate"` // {0, 30} if zero fine, otherwise reduce framerate
}

type FFmpeg struct {
	Src       string  `json:"src_path"`
	Tmp_path  string  `json:"tmp_path"`
	Tmp2_path string  `json:"tmp2_path"`
	Outpath   string  `json:"out_path"`
	Opts      Options `json:"opts"`
	Pass      int     `json:"pass"`
	Nullpath  string  `json:"null_path"`
}

// ############## FFProbe methods ##############
func FF_Probe(filepath string) *FFProbeResult {
	cmd := exec.Command("ffprobe",
		"-v", "error", // Suppress verbose output, only show errors
		"-print_format", "json", // Request JSON output
		"-show_format",  // Show container format information
		"-show_streams", // Show stream information (video, audio, etc.)
		filepath,
	)

	output, err := cmd.Output()
	if err != nil {
		log.Fatalf("Error running ffprobe: %v\nOutput: %s", err, output)
	}

	ff_probe_res := new(FFProbeResult)
	err = json.Unmarshal(output, ff_probe_res)
	if err != nil {
		log.Fatalf("Error unmarshaling ffprobe JSON output: %v", err)
	}

	return ff_probe_res

}

var br_map = map[int]int{
	307200:  1e6, // 480 x 640
	921600:  2e6, // 720 x 1280
	2073600: 5e6, // 1080 x 1920
}

func map_br_to_string(br int) string {
	if br < 1e3 {
		return fmt.Sprintf("%d", br)
	}

	if br < 1e6 {
		return fmt.Sprintf("%dk", br/1000)
	}

	return fmt.Sprintf("%dM", br/1e6)
}

func nearest_thr(wh int) int {
	if wh <= 307200 {
		return 307200
	}

	if wh <= 921600 {
		return 921600
	}

	return 2073600
}

// determine option set for ffmpeg
func (ffp *FFProbeResult) determine_options() Options {

	// get current bitrate bits/sec
	br, err := strconv.Atoi(ffp.Format.BitRate)
	if err != nil {
		log.Fatal("Failed to convert bitrate to integer:", err)
	}

	idx := -1
	for i, s := range ffp.Streams {
		if s.CodecType == "video" {
			idx = i
			break
		}
	}

	if idx == -1 {
		log.Fatalf("no video stream in file: %s\n", ffp.Format.Filename)
	}

	// get video width in pxls
	width, height := ffp.Streams[idx].Width, ffp.Streams[idx].Height
	pxl_frame := width * height

	// get framerate
	frames, err := strconv.Atoi(ffp.Streams[idx].NBFrames)
	if err != nil {
		log.Fatal("Failed to convert frames to integer:", err)
	}

	// duration in seconds
	dur, err := strconv.ParseFloat(ffp.Format.Duration, 64)
	if err != nil {
		log.Fatal("Failed to convert duration to float:", err)
	}

	fps := float64(frames) / dur

	out_opts := Options{Proc: false, Bitrate: "", Framerate: 0}

	nearest := nearest_thr(pxl_frame)
	if br > br_map[nearest] {
		out_opts.Proc = true
		out_opts.Bitrate = map_br_to_string(br_map[nearest])
	}

	if fps > 35 {
		out_opts.Proc = true
		out_opts.Framerate = 30
	}

	return out_opts

}

// #############################################

// ################## FFMpeg ###################

// ffmpeg object constructor
func New_FFMpeg(src string, outpath string) FFmpeg {
	probe := FF_Probe(src)
	opts := probe.determine_options()
	null_str := "/dev/null"
	if is_windows_os() {
		null_str = "NUL"
	}
	return FFmpeg{
		Src:       src,
		Tmp_path:  "_ffmpeg_tmp.mp4",
		Tmp2_path: "_ffmpeg_tmp2.mp4",
		Outpath:   outpath,
		Opts:      opts,
		Pass:      1,
		Nullpath:  null_str,
	}
}

// debug helper; dump FFmpeg object as JSON
func debug_print(str io.Writer, obj any) {
	jsonOutput, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		fmt.Println("Error marshalling obj to JSON:", err)
		return
	}

	if _, e := fmt.Fprintf(str, "%s\n", string(jsonOutput)); e != nil {
		log.Fatal(e)
	}
}

// copy file to destination
func (ffm FFmpeg) copy_file() error {
	srcFile, err := os.Open(ffm.Src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	make_dir(ffm.Outpath)
	dest_path := filepath.Join(ffm.Outpath, filepath.Base(ffm.Src))

	destFile, err := os.Create(dest_path)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		return err
	}

	return nil
}

// move the resultant local temp file to destination path
func (ffm FFmpeg) save_result() error {

	// make out dir if not exists
	make_dir(ffm.Outpath)

	dest_path := filepath.Join(ffm.Outpath, filepath.Base(ffm.Src))
	fmt.Printf("saving to: %s\n", dest_path)
	err := os.Rename(ffm.Tmp_path, dest_path)
	if err != nil {
		return err
	}

	return nil

}

func (ffm FFmpeg) reduce_bitrate() error {

	// source file toggle
	use_path := ffm.Src
	save_path := ffm.Tmp_path
	if ffm.Pass > 1 {
		use_path = ffm.Tmp_path
		save_path = ffm.Tmp2_path
		fmt.Printf("using source and save: %s; %s\n", use_path, save_path)
	}

	pass1cmd := exec.Command("ffmpeg",
		"-y",
		"-i", use_path, // Input file
		"-pass", "1",
		"-b:v", ffm.Opts.Bitrate,
		"-f", "mp4", // Output format for the pass
		"-an",                // No audio for pass 1
		"-loglevel", "error", // Suppress excessive logging
		ffm.Nullpath,
	)

	pass2cmd := exec.Command("ffmpeg",
		"-y",
		"-i", use_path, // Input file
		"-pass", "2",
		"-b:v", ffm.Opts.Bitrate,
		"-c:a", "copy",
		save_path,
	)

	// pass one
	if err := pass1cmd.Run(); err != nil {
		fmt.Printf("Error during Pass 1\n%s\n", strings.Join(pass1cmd.Args, ", "))
		return err
	}
	log.Print("bitrate reduction analysis Pass 1 complete\n")

	// pass two
	if err := pass2cmd.Run(); err != nil {
		fmt.Printf("Error during Pass 2\n%s\n", strings.Join(pass2cmd.Args, ", "))
		return err
	}
	log.Print("bitrate reduction analysis Pass 2 complete\n")

	// clean up temp files
	if ffm.Pass > 1 {
		err := os.Rename(ffm.Tmp2_path, ffm.Tmp_path)
		if err != nil {
			return err
		}
	}

	// clean up log files
	if err := os.Remove("ffmpeg2pass-0.log"); err != nil {
		log.Printf("could not remove: ffmpeg2pass-0.log: %s\n", err)
	}
	if err := os.Remove("ffmpeg2pass-0.log.mbtree"); err != nil {
		log.Printf("could not remove: ffmpeg2pass-0.log.mbtree: %s\n", err)
	}

	return nil
}

// done before bitrate reduction if necessary (2-pass)
func (ffm FFmpeg) reduce_framerate() error {
	use_path := ffm.Src
	if ffm.Pass > 1 {
		use_path = ffm.Tmp_path
		fmt.Printf("using source: %s\n", use_path)
	}

	cmdArgs := []string{
		"-y",
		"-i", use_path, // Input file
		"-filter:v", fmt.Sprintf("fps=%d", ffm.Opts.Framerate),
		"-c:a", "copy",
		"_ffmpeg_tmp.mp4", // Output file
	}

	cmd := exec.Command("ffmpeg", cmdArgs...)
	err := cmd.Run()
	if err != nil {
		log.Print("FFmpeg reduce framerate command failed\n")
		return err
	}

	return nil
}

// process a file based on the options present until no processing is necessary
func (ffm *FFmpeg) process_file() error {
	fmt.Printf("working on: %s\n", ffm.Src)
	// no op copy
	if !ffm.Opts.Proc {
		fmt.Println("copying; nothing to do")
		if err := ffm.copy_file(); err != nil {
			return err
		}
		return nil
	}

	for ffm.Opts.Proc {

		// bitrate
		if ffm.Opts.Framerate == 0 && ffm.Opts.Bitrate != "" {
			fmt.Printf("reducing bitrate: pass: %d\n", ffm.Pass)
			if err := ffm.reduce_bitrate(); err != nil {
				return err
			}
			ffm.Opts.Proc = false
		}

		// frame rate reduction
		if ffm.Opts.Framerate > 0 {
			fmt.Printf("reducing framerate: pass: %d\n", ffm.Pass)
			if err := ffm.reduce_framerate(); err != nil {
				return err
			}
			tmp_probe := FF_Probe(ffm.Tmp_path)
			ffm.Opts = tmp_probe.determine_options()
			ffm.Pass++
			debug_print(os.Stdout, ffm.Opts)
		}

	}

	if err := ffm.save_result(); err != nil {
		os.Remove(ffm.Tmp_path)
		return err
	}

	return nil
}

// #############################################

// is windows
func is_windows_os() bool {
	os := runtime.GOOS

	switch os {
	case "windows":
		return true
	default:
		return false
	}

}

// check to see if dir exists
func dir_exists(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil // Directory does not exist
	}
	if err != nil {
		return false, err // Other error (e.g., permissions)
	}
	return info.IsDir(), nil // Return true if it's a directory, false otherwise
}

// create a dir if it doesn't exist
func make_dir(path string) {
	exists, err := dir_exists(path)
	if err != nil {
		log.Fatalf("could not check directory %s\n%s\n", path, err)
	}
	if exists {
		return
	}

	log.Printf("making output direcotry %s\n", path)
	if err := os.MkdirAll(path, 0755); err != nil {
		log.Fatalf("could not create dir: %s\n", err)
	}
}
