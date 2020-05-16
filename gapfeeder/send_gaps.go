package main

import (
	"bufio"
	// "compress/gzip"
	"flag"
	"fmt"
	gzip "github.com/klauspost/pgzip"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// only send email attachments smaller than this size
	maxCompressedSize = 10 * 1024 * 1024
	mailxCmd = "echoargs"
)

var (
	debug             bool
	recipients        string
	gapDir            string
	hostname          string
	gapCountName      string
	gapFileGlob       string
	subjectLine       string
	fqnGapFile        string
	compressedSize    int64
	maxTransferSize   int64
	fqnCompressedFile *os.File
)

type byModificationTime []os.FileInfo

func (b byModificationTime) Len() int           { return len(b) }
func (b byModificationTime) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byModificationTime) Less(i, j int) bool { return b[i].ModTime().Before(b[j].ModTime()) }

func main() {

	parseArgs()
    readGapFiles(&fqnGapFile)
	fqnCompressedFile = compress(&compressedSize)
	lastLines := getRecentGapMessages()
	mailArgs := createMailArgs()
	sendMailWithAttachment(lastLines, mailArgs)

}

func parseArgs() {

	// flag.StringVar(&recipients, "recipients", "support@unidata.ucar.edu", "where to send the email to")
	flag.StringVar(&recipients, "recipients", "evan.polster@noaa.gov", "where to send the email to")
	flag.StringVar(&gapDir, "gap-directory", "/usr/local/ldm/logs", "directory to search for .gap files")
	flag.StringVar(&hostname, "hostname", "", "hostname of this machine, (default will be computed)")
	flag.StringVar(&gapCountName, "gap-count-name", "", "name of the .gap count file, (default $hostname_gapcount)")
	flag.StringVar(&gapFileGlob, "gap-file-glob", "", "glob to search .gap files with, (default $hostname_*.gap)")
	flag.StringVar(&subjectLine, "subject", "", "subject of the email being sent out, (default includes the machine's hostname)")
	flag.BoolVar(&debug,"debug-mode",false,"shows debug echos when true")
	flag.Int64Var(&maxTransferSize, "max-xfer-allowed",maxCompressedSize, "maximum transfer size allowed (default 10 MB")
	flag.Parse()

	// computed defaults instead of static ones
	var err error
	if hostname == "" {
		hostname, err = os.Hostname()
		if err != nil {
			panic(err)
		}
		if strings.Index(hostname, ".") > 0 {
			hostname = strings.Split(hostname, ".")[0]
		}
	}
	if gapCountName == "" {
		gapCountName = hostname + "_gapcount"
	}
	if gapFileGlob == "" {
		gapFileGlob = hostname + "_*.gap"
	}
	if subjectLine == "" {
		subjectLine = "Gap-in-sequence messages from " + hostname
	}

	if maxTransferSize > maxCompressedSize {
		maxTransferSize = maxCompressedSize
	}

	dumpArgs("Program Start:")
}

func readGapFiles(attachment *string) {
	fmt.Println("... Reading gap files")

	directory, err := os.Open(gapDir)
	if err != nil {
		panic(err)
	}
	defer directory.Close()

	// read all information for all files from gap file directory
	files, err := directory.Readdir(-1)
	if err != nil {
		panic(err)
	}

	// filter file names by glob, error out if there were no matches
	var matched []os.FileInfo
	for _, f := range files {
		if ok, err := filepath.Match(gapFileGlob, f.Name()); err != nil {
			panic(err)
		} else if ok {
			matched = append(matched, f)
		}
	}
	if len(matched) == 0 {
		fmt.Fprintf(os.Stderr, "Couldn't find any .gap files in %v matching %v, aborting\n", gapDir, gapFileGlob)
		os.Exit(1)
	}

	sort.Sort(sort.Reverse(byModificationTime(matched)))
	lastModifiedGapFile := matched[0]
	*attachment = filepath.Join(gapDir, lastModifiedGapFile.Name())

}
func compress(compressSize *int64) *os.File {
	fmt.Println("... Compressing gap file attachment")

	// e.g. change it with env TMPDIR=/mnt/... go run send_gaps.go ...
	tmpfile, err := ioutil.TempFile("", "gap")
	if err != nil {
		panic(err)
	}
	defer os.Remove(tmpfile.Name())

	uncompressed, err := os.Open(fqnGapFile)
	if err != nil {
		panic(err)
	}

	compressor, err := gzip.NewWriterLevel(tmpfile, gzip.BestCompression)
	if err != nil {
		panic(err)
	}

	_, err = io.Copy(compressor, uncompressed)
	if err != nil {
		panic(err)
	}

	err = compressor.Close()
	if err != nil {
		panic(err)
	}

	err = tmpfile.Sync()
	if err != nil {
		panic(err)
	}

	stat, err := tmpfile.Stat()
	if err != nil {
		panic(err)
	}
	*compressSize = stat.Size()

	return tmpfile
}

func getRecentGapMessages() []string {
	fmt.Println("... Gathering the previous gap messages for mail body")

	// open gap count file and make sure to close it again
	gapCountFile, err := os.Open(filepath.Join(gapDir, gapCountName))
	if err != nil {
		panic(err)
	}
	defer gapCountFile.Close()

	// efficiently capture last three lines of gap count file, including newlines
	lastLines := make([]string, 3)
	reader := bufio.NewReader(gapCountFile)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		lastLines[0], lastLines[1], lastLines[2] = lastLines[2], lastLines[1], line
	}

	return lastLines
}

func createMailArgs() []string {
	fmt.Println("... Creating mail arguments")

	mailArgs := []string{"-s"}


	echoln("\t >>> Compressed size:", compressedSize)

	if compressedSize > maxCompressedSize {
		echoln("\t >>>>>> Size is over the transfer limit; compressed gap file (.gz) with not be attached")
		subjectLine += " without gap file attachment"
		mailArgs = append(mailArgs, subjectLine)
	} else {
		mailArgs = append(mailArgs, subjectLine, "-a", fqnCompressedFile.Name())
	}

	mailArgs = append(mailArgs, recipients)

	return mailArgs
}

func sendMailWithAttachment(lastLines []string, mailArgs []string) {

	fmt.Println("... Sending mail")

	dumpMailArgs("Presend Mail Arguments", mailArgs)

	// run external mail program and wait for result
	cmd := exec.Command(mailxCmd, mailArgs...)
	cmd.Stdin = strings.NewReader(lastLines[0] + lastLines[1])
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		exitError, ok := err.(*exec.ExitError)
		if ok {
			os.Exit(exitError.ExitCode())
		} else {
			panic(err)
		}
	}

}

func dumpArgs(header string) {

	if debug {
		fmt.Println()
		if len(header) > 0 {
			fmt.Println(" ", header)
			fmt.Print("*")
			for i := 0; i < len(header)+1; i++ {
				fmt.Print("*")
			}
			fmt.Println("*")
		}

		fmt.Print("Recipients: ")
		fmt.Printf("%#v\n", recipients)

		fmt.Print("Gap File Directory: ")
		fmt.Printf("%#v\n", gapDir)

		fmt.Print("Hostname: ")
		fmt.Printf("%#v\n", hostname)

		fmt.Print("Gap Count File Name: ")
		fmt.Printf("%#v\n", gapCountName)

		fmt.Print("Gap File Glob: ")
		fmt.Printf("%#v\n", gapFileGlob)

		fmt.Print("Subject line: ")
		fmt.Printf("%#v\n", subjectLine)

		fmt.Print("Maximum Transfer in Bytes: ")
		fmt.Printf("%#v\n", maxTransferSize)

		fmt.Println()
	}
}

func dumpMailArgs(header string, mailArgs []string) {

	if debug {
		fmt.Println()
		if len(header) > 0 {
			fmt.Println(" ", header)
			fmt.Print("*")
			for i := 0; i < len(header)+1; i++ {
				fmt.Print("*")
			}
			fmt.Println("*")
		}

		fmt.Println()
		for _, v := range mailArgs {
			fmt.Print(v, " ")
		}
		fmt.Println()
	}

}

func echo(s ...interface{}) {
	if debug {
		fmt.Print(s...)
	}
}
func echoln(s ...interface{}) {
	if debug {
		fmt.Println(s...)
	}
}
