package img

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	containerregistryv1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/internal/manifest/filemutex"
)

var (
	ErrImageLayerPull       = errors.New("failed to pull layer")
	ErrInvalidImageSpecType = fmt.Errorf("invalid image spec type provided,"+
		" only '%s' '%s' are allowed", v1beta2.OciRefType, v1beta2.OciDirType)
	ErrTaintedArchive          = errors.New("content filepath tainted")
	ErrInvalidArchiveStructure = errors.New("tar archive has invalid structure, expected a single file")
)

type PathExtractor struct {
	fileMutexCache *filemutex.MutexCache
}

func NewPathExtractor() *PathExtractor {
	return &PathExtractor{fileMutexCache: filemutex.NewMutexCache(nil)}
}

func (p PathExtractor) GetPathFromRawManifest(ctx context.Context, imageSpec v1beta2.ImageSpec, keyChain authn.Keychain) (string, error) {
	switch imageSpec.Type {
	case v1beta2.OciRefType:
		return p.GetPathForFetchedLayer(ctx, imageSpec, keyChain, string(v1beta2.RawManifestLayer)+".yaml")
	case v1beta2.OciDirType:
		tarFile, err := p.GetPathForFetchedLayer(ctx, imageSpec, keyChain, string(v1beta2.RawManifestLayer)+".tar")
		if err != nil {
			return "", err
		}
		extractedFile, err := p.ExtractLayer(tarFile)
		if err != nil {
			return "", err
		}
		return extractedFile, nil
	default:
		return "", ErrInvalidImageSpecType
	}
}

func (p PathExtractor) GetPathForFetchedLayer(ctx context.Context,
	imageSpec v1beta2.ImageSpec,
	keyChain authn.Keychain,
	filename string,
) (string, error) {
	imageRef := fmt.Sprintf("%s/%s@%s", imageSpec.Repo, imageSpec.Name, imageSpec.Ref)

	installPath := getFsChartPath(imageSpec)
	manifestPath := path.Join(installPath, filename)

	fileMutex, err := p.fileMutexCache.GetLocker(installPath)
	if err != nil {
		return "", fmt.Errorf("failed to load locker from cache: %w", err)
	}
	fileMutex.Lock()
	defer fileMutex.Unlock()

	dir, err := os.Open(manifestPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("opening dir for installs caused an error %s: %w", imageRef, err)
	}
	if dir != nil {
		return manifestPath, nil
	}

	imgLayer, err := pullLayer(ctx, imageRef, keyChain)
	if err != nil {
		return "", err
	}

	// copy uncompressed manifest to install path
	blobReadCloser, err := imgLayer.Uncompressed()
	if err != nil {
		return "", fmt.Errorf("failed fetching blob for layer %s: %w", imageRef, err)
	}
	defer blobReadCloser.Close()

	// create dir for uncompressed manifest
	if err := os.MkdirAll(installPath, fs.ModePerm); err != nil {
		return "", fmt.Errorf(
			"failure while creating installPath directory for layer %s: %w",
			imageRef, err,
		)
	}
	outFile, err := os.Create(manifestPath)
	if err != nil {
		return "", fmt.Errorf("file create failed for layer %s: %w", imageRef, err)
	}
	if _, err := io.Copy(outFile, blobReadCloser); err != nil {
		return "", fmt.Errorf("file copy storage failed for layer %s: %w", imageRef, err)
	}
	err = io.Closer(outFile).Close()
	if err != nil {
		return manifestPath, fmt.Errorf("failed to close io: %w", err)
	}
	return manifestPath, nil
}

func (p PathExtractor) ExtractLayer(tarPath string) (string, error) {
	fileMutex, err := p.fileMutexCache.GetLocker(tarPath)
	if err != nil {
		return "", fmt.Errorf("failed to load locker from cache: %w", err)
	}
	fileMutex.Lock()
	defer fileMutex.Unlock()

	tarFile, err := os.Open(tarPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer tarFile.Close()

	tarReader := tar.NewReader(tarFile)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read tar: %w", err)
		}

		if header.Typeflag == tar.TypeReg {
			// On macOS, tar files generated by default include a copyfile that starts with ._.
			// This condition skips those files.
			if strings.HasPrefix(header.Name, "._") {
				continue
			}
			extractedFilePath, err := sanitizeArchivePath(filepath.Dir(tarPath), header.Name)
			if err != nil {
				return "", fmt.Errorf("failed to sanitize archive path: %w", err)
			}

			if _, err := os.Stat(extractedFilePath); err == nil {
				return extractedFilePath, nil
			}

			outFile, err := os.Create(extractedFilePath)
			if err != nil {
				return "", fmt.Errorf("failed to create extracted file: %w", err)
			}
			defer outFile.Close()

			if _, err := io.Copy(outFile, tarReader); err != nil { //nolint:gosec // The upstream content is
				// from managed resources, and the size is controlled, so it is safe from decompression bomb attacks.
				return "", fmt.Errorf("failed to extract from tar: %w", err)
			}
			return extractedFilePath, nil
		}
	}
	return "", ErrInvalidArchiveStructure
}

func pullLayer(ctx context.Context, imageRef string, keyChain authn.Keychain) (containerregistryv1.Layer, error) {
	noSchemeImageRef := noSchemeURL(imageRef)
	isInsecureLayer, err := regexp.MatchString("^http://", imageRef)
	if err != nil {
		return nil, fmt.Errorf("invalid imageRef: %w", err)
	}

	if isInsecureLayer {
		imgLayer, err := crane.PullLayer(noSchemeImageRef, crane.Insecure, crane.WithAuthFromKeychain(keyChain))
		if err != nil {
			return nil, fmt.Errorf("%s due to: %w", ErrImageLayerPull.Error(), err)
		}
		return imgLayer, nil
	}

	imgLayer, err := crane.PullLayer(noSchemeImageRef, crane.WithAuthFromKeychain(keyChain), crane.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("%s due to: %w", ErrImageLayerPull.Error(), err)
	}
	return imgLayer, nil
}

func getFsChartPath(imageSpec v1beta2.ImageSpec) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%s", imageSpec.Name, imageSpec.Ref))
}

// sanitizeArchivePath ensures the path is within the intended directory to prevent path traversal attacks (gosec:G305).
func sanitizeArchivePath(dir, path string) (string, error) {
	joinedPath := filepath.Join(dir, path)
	if strings.HasPrefix(joinedPath, filepath.Clean(dir)) {
		return joinedPath, nil
	}

	return "", fmt.Errorf("%w: %s", ErrTaintedArchive, path)
}

func noSchemeURL(url string) string {
	regex := regexp.MustCompile(`^https?://`)
	return regex.ReplaceAllString(url, "")
}
