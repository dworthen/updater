package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/google/uuid"
)

type NotSupportedError struct {
	Platform string
}

func (ns *NotSupportedError) Error() string {
	return fmt.Sprintf("Self updating is not support for %s.", ns.Platform)
}

type UpdaterManifest struct {
	Version string                       `json:"Version"`
	Archive string                       `json:"archive"`
	Binary  string                       `json:"binary"`
	Os      map[string]string            `json:"os"`
	Arch    map[string]map[string]string `json:"arch"`
}

type UpdaterConfig struct {
	CurrentVersion string
	BaseUrl        string
	UpdaterConfig  string
}

type Updater struct {
	config      *UpdaterConfig
	archiveName string
	binaryName  string
}

func New(config *UpdaterConfig) *Updater {
	return &Updater{
		config: config,
	}
}

func validateUrl(path string) error {
	url, err := url.Parse(path)
	if err != nil {
		return err
	}

	if url.Scheme != "https" {
		return fmt.Errorf("Invalid URL scheme. Only https is supported")
	}

	if url.Host == "" {
		return fmt.Errorf("Invalud URL. Expecting absolute URL but got %s", path)
	}

	return nil
}

func (updater *Updater) GetManifest() (*UpdaterManifest, error) {
	requestUrl, err := url.JoinPath(updater.config.BaseUrl, updater.config.UpdaterConfig)
	if err != nil {
		return nil, err
	}

	err = validateUrl(requestUrl)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest("GET", requestUrl, nil)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{}
	resp, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Error getting updater config. Status code: %d", resp.StatusCode)
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var manifest UpdaterManifest
	err = json.Unmarshal(responseBody, &manifest)
	if err != nil {
		return nil, err
	}

	return &manifest, nil
}

func (updater *Updater) CheckForAvailableUpdate() (bool, string, error) {
	currentVersion := strings.TrimSpace(updater.config.CurrentVersion)
	if currentVersion == "" {
		return false, "", fmt.Errorf("Current version not specified")
	}

	manifest, err := updater.GetManifest()
	if err != nil {
		return false, "", err
	}

	manifestVersion := strings.TrimSpace(manifest.Version)

	if currentVersion != manifestVersion {
		return true, manifestVersion, nil
	}

	return false, "", nil
}

type variables struct {
	Os         string
	Arch       string
	ArchiveExt string
	Ext        string
}

func (manifest *UpdaterManifest) GetDownloadInfo() (string, string, error) {
	if strings.TrimSpace(manifest.Binary) == "" {
		return "", "", fmt.Errorf("Manifest does not specify binary name")
	}

	os := runtime.GOOS
	archiveExt := ".tar.gz"
	arch := runtime.GOARCH
	notSupported := &NotSupportedError{
		Platform: fmt.Sprintf("%s/%s", os, arch),
	}
	ext := ""
	if os == "windows" {
		archiveExt = ".zip"
		ext = ".exe"
	}

	os, ok := manifest.Os[os]
	if !ok {
		return "", "", notSupported
	}

	archMap, ok := manifest.Arch[os]
	if !ok {
		return "", "", notSupported
	}

	arch, ok = archMap[arch]
	if !ok {
		return "", "", notSupported
	}

	variables := variables{
		Os:         os,
		Arch:       arch,
		ArchiveExt: archiveExt,
		Ext:        ext,
	}

	archiveName := ""
	binaryName := ""

	if strings.TrimSpace(manifest.Archive) != "" {
		archiveTmpl, err := template.New("ArchiveTemplate").Parse(manifest.Archive)
		if err != nil {
			return "", "", err
		}
		var buf bytes.Buffer
		err = archiveTmpl.Execute(&buf, variables)
		if err != nil {
			return "", "", err
		}
		archiveName = buf.String()
	}

	binaryTmpl, err := template.New("BinaryTemplate").Parse(manifest.Binary)
	if err != nil {
		return "", "", err
	}
	var buf bytes.Buffer
	err = binaryTmpl.Execute(&buf, variables)
	if err != nil {
		return "", "", err
	}
	binaryName = buf.String()

	return archiveName, binaryName, nil
}

func (updater *Updater) Update() error {
	manifest, err := updater.GetManifest()
	if err != nil {
		return err
	}

	archiveName, binaryName, err := manifest.GetDownloadInfo()
	if err != nil {
		return err
	}

	updater.archiveName = archiveName
	updater.binaryName = binaryName

	if archiveName != "" {
		return updater.downloadArchive()
	} else {
		return updater.downloadBinary()
	}
}

func (updater *Updater) downloadBinary() error {
	tempDir := os.TempDir()
	filename := uuid.NewString()
	tempFile := filepath.Join(tempDir, filename)
	binaryPath, err := os.Executable()
	if err != nil {
		return err
	}

	requestUrl, err := url.JoinPath(updater.config.BaseUrl, updater.binaryName)
	if err != nil {
		return err
	}

	err = validateUrl(requestUrl)
	if err != nil {
		return err
	}

	request, err := http.NewRequest("GET", requestUrl, nil)
	if err != nil {
		return err
	}

	httpClient := &http.Client{}
	resp, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Error getting updater config. Status code: %d", resp.StatusCode)
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = os.Rename(binaryPath, tempFile)
	if err != nil {
		return err
	}

	err = os.WriteFile(binaryPath, responseBody, 0644)
	if err != nil {
		return err
	}
	err = os.Chmod(binaryPath, 0744)
	if err != nil {
		return err
	}

	return nil
}

func (updater *Updater) downloadArchive() error {
	tempDir := os.TempDir()
	filename := uuid.NewString()
	tempFile := filepath.Join(tempDir, filename)

	requestUrl, err := url.JoinPath(updater.config.BaseUrl, updater.archiveName)
	if err != nil {
		return err
	}

	err = validateUrl(requestUrl)
	if err != nil {
		return err
	}

	request, err := http.NewRequest("GET", requestUrl, nil)
	if err != nil {
		return err
	}

	httpClient := &http.Client{}
	resp, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Error getting updater config. Status code: %d", resp.StatusCode)
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = os.WriteFile(tempFile, responseBody, 0644)
	if err != nil {
		return err
	}

	if strings.HasSuffix(strings.ToLower(updater.archiveName), ".tar.gz") {
		return updater.extractTarball(tempFile)
	} else if strings.HasSuffix(strings.ToLower(updater.archiveName), ".zip") {
		return updater.extractZip(tempFile)
	} else {
		return fmt.Errorf("Error. Only .tar.gz or .zip archives are supported. Got %s", updater.archiveName)
	}
}

func (updater *Updater) extractZip(src string) error {
	tempDir := os.TempDir()
	destination := filepath.Dir(src)
	binaryPath, err := os.Executable()
	if err != nil {
		return err
	}

	uncompressedStream, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("ExtractZip: NewReader failed %w", err)
	}

	created := false

	for _, f := range uncompressedStream.File {
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("ExtractZip: failed to open file %w", err)
		}

		basename := filepath.Base(f.Name)

		if basename == updater.binaryName {
			filename := uuid.NewString()
			tempName := uuid.NewString()
			tempPath := filepath.Join(tempDir, tempName)

			path := filepath.Join(destination, filename)

			if f.FileInfo().Mode().IsRegular() {
				file, err := os.Create(path)
				if err != nil {
					return fmt.Errorf("ExtractZip: failed to open file %w", err)
				}

				_, err = io.Copy(file, rc)
				if err != nil {
					return fmt.Errorf("ExtractZip: failed to copy file %w", err)
				}
				err = file.Close()
				if err != nil {
					return err
				}
				rc.Close()
				err = os.Rename(binaryPath, tempPath)
				if err != nil {
					return err
				}

				err = os.Rename(path, binaryPath)
				if err != nil {
					return err
				}
				err = os.Chmod(binaryPath, 0744)
				if err != nil {
					return err
				}
				created = true
				break
			}
		}
	}

	if !created {
		return fmt.Errorf("Error extracting binary from %s. No binary matched the name %s", updater.archiveName, updater.binaryName)
	}

	return nil
}

func (updater *Updater) extractTarball(src string) error {
	tempDir := os.TempDir()
	destination := filepath.Dir(src)
	binaryPath, err := os.Executable()
	if err != nil {
		return err
	}

	file, err := os.Open(src)
	if err != nil {
		return err
	}

	uncompressedStream, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("ExtractTarGz: NewReader failed %w", err)
	}

	tarReader := tar.NewReader(uncompressedStream)

	created := false

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("ExtractTarGz: Next() failed: %w", err)
		}

		basename := filepath.Base(header.Name)

		if basename == updater.binaryName {
			filename := uuid.NewString()
			tempName := uuid.NewString()
			tempPath := filepath.Join(tempDir, tempName)

			path := filepath.Join(destination, filename)

			switch header.Typeflag {
			case tar.TypeReg:
				outFile, err := os.Create(path)
				if err != nil {
					return fmt.Errorf("ExtractTarGz: Create() failed: %w", err)
				}
				if _, err := io.Copy(outFile, tarReader); err != nil {
					return fmt.Errorf("ExtractTarGz: Copy() failed: %w", err)
				}
				err = outFile.Close()
				if err != nil {
					return fmt.Errorf("Failed to close file. %w", err)
				}
				err = os.Rename(binaryPath, tempPath)
				if err != nil {
					return err
				}
				err = os.Rename(path, binaryPath)
				if err != nil {
					return err
				}
				err = os.Chmod(binaryPath, 0744)
				if err != nil {
					return err
				}
				created = true
				break
			default:
				continue
			}
		}

	}

	if !created {
		return fmt.Errorf("Error extracting binary from %s. No binary matched the name %s", updater.archiveName, updater.binaryName)
	}

	return os.Remove(src)
}
