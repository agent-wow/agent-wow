// Command clientdata downloads pinned public references for the catalogue
// generators into the repository's gitignored tmp/client-data directory.
// Run from the repository root with ./run client-data:fetch.
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"time"

	"github.com/hazim-j/agent-wow/tools/internal/clientdata"
)

type source struct {
	name, sha256 string
}

var dbcSources = []source{
	{"CharSections.dbc", "18fc8d23f6e66598ab1681a74f90239ebdf84b76f300db0865cdad7d042b833f"},
	{"CharHairGeosets.dbc", "02421880eff0960acd722324a57546612edd05d76be9a0d77d273cffe9fba843"},
	{"CharacterFacialHairStyles.dbc", "d67bfb0a674af83dab5301ab686a534ec888fae7b52a91045213e111a2399554"},
	{"Map.dbc", "261bd7c2c9dbdba1f2966ca21106673ed722c2e3681d227f1eed261b6ce870d5"},
	{"AreaTable.dbc", "eb4bcfa77b03aed853c4783ac260a4259970effaad5b8a74d868783b4dbdc44e"},
}

var archiveSource = source{"Data.zip", "a3d4df635ae6c2c8f08052c32a79e0f806955150ad36b014a823dd08a32a4610"}
var classesSource = source{"playercreateinfo.sql", "0d74fddfeaac43fc07da188d1b6244a30088ca01b4d598d4ca7d317f619fdeac"}
var opcodesSource = source{"Opcodes.h", clientdata.OpcodesSHA256}

const maxReferenceSize = 16 << 20

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("Usage: ./run client-data:fetch\nDownloads the v20.0 client archive (~1.2 GB) and pinned creation and opcode references to tmp/client-data.")
		return
	}
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: ./run client-data:fetch")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Minute}
	if err := fetch(ctx, client, clientdata.Directory, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "fetch client data:", err)
		os.Exit(1)
	}
}

func fetch(ctx context.Context, client *http.Client, directory string, out io.Writer) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	if err := download(ctx, client, clientdata.ArchiveURL, directory, archiveSource, 1196168257, out); err != nil {
		return err
	}
	if err := extract(filepath.Join(directory, archiveSource.name), directory, dbcSources); err != nil {
		return fmt.Errorf("extract client tables: %w", err)
	}
	if err := download(ctx, client, clientdata.ClassesURL, directory, classesSource, maxReferenceSize, out); err != nil {
		return err
	}
	if err := download(ctx, client, clientdata.OpcodesURL, directory, opcodesSource, maxReferenceSize, out); err != nil {
		return err
	}
	fmt.Fprintf(out, "Client data ready in %s. Run './run charcatalog:generate', './run mapcatalog:generate', or './run opcodecatalog:generate'.\n", directory)
	return nil
}

func download(ctx context.Context, client *http.Client, url, directory string, src source, limit int64, out io.Writer) error {
	destination := filepath.Join(directory, src.name)
	if validFile(destination, src.sha256) {
		fmt.Fprintf(out, "Using verified %s\n", destination)
		return nil
	}
	fmt.Fprintf(out, "Downloading %s\n", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %s", src.name, response.Status)
	}
	if response.ContentLength > limit {
		return fmt.Errorf("download %s exceeds size limit", src.name)
	}
	return writeVerified(directory, src, response.Body, limit)
}

func validFile(filename, checksum string) bool {
	file, err := os.Open(filename)
	if err != nil {
		return false
	}
	defer file.Close()
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	return err == nil && fmt.Sprintf("%x", hash.Sum(nil)) == checksum
}

// Write beside the destination and rename only after validation. Interrupted
// downloads and failed checksums cannot replace a previously complete file.
func writeVerified(directory string, src source, reader io.Reader, limit int64) error {
	file, err := os.CreateTemp(directory, ".fetch-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(reader, limit+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", src.name, err)
	}
	if n > limit {
		return fmt.Errorf("%s exceeds size limit", src.name)
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != src.sha256 {
		return fmt.Errorf("%s: SHA-256 checksum mismatch", src.name)
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(directory, src.name))
}

func extract(filename, directory string, sources []source) error {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer archive.Close()
	for _, src := range sources {
		var found *zip.File
		for _, file := range archive.File {
			if path.Base(file.Name) != src.name {
				continue
			}
			if found != nil {
				return fmt.Errorf("archive contains duplicate %s", src.name)
			}
			found = file
		}
		if found == nil {
			return fmt.Errorf("archive is missing %s", src.name)
		}
		if !found.Mode().IsRegular() || found.UncompressedSize64 > maxReferenceSize {
			return fmt.Errorf("archive has invalid file type or size for %s", src.name)
		}
		reader, err := found.Open()
		if err != nil {
			return err
		}
		// Only fixed source names become destinations; archive paths are never
		// joined to the output directory, preventing traversal outside it.
		err = writeVerified(directory, src, reader, maxReferenceSize)
		if err = errors.Join(err, reader.Close()); err != nil {
			return err
		}
	}
	return nil
}
