package main

import (
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nlepage/go-tarfs"
)

// File represents the structure of a file entry in files.xml
type File struct {
	ID          string `xml:"id,attr"`
	ContentHash string `xml:"contenthash"`
	Filename    string `xml:"filename"`
	Folder      string `xml:"-"` // Ignore Folder when XML parsing
}

var forbidden = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]+`)

// sanitizeFileName replaces invalid characters in folder names with a hyphen.
// This is used to ensure that folder names are valid for file systems.
func sanitizeFileName(fileName string) string {
	return forbidden.ReplaceAllString(fileName, "")
}

// parseXMLFile reads XML data from an io.Reader and unmarshals it into the provided struct.
// It returns an error if the data cannot be read or parsed.
func parseXMLFile(reader io.Reader, v interface{}) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	return xml.Unmarshal(data, v)
}

// buildFileMapping reads the files.xml file and builds a mapping of file IDs to File structs.
// It returns a map where the keys are file IDs and the values are File structs.
func buildFileMapping(source fs.FS, filesXMLPath string) (map[string]File, error) {
	file, err := source.Open(filesXMLPath)
	if err != nil {
		return nil, fmt.Errorf("error reading files.xml: %w", err)
	}
	defer file.Close()

	var files struct {
		Files []File `xml:"file"`
	}
	if err := parseXMLFile(file, &files); err != nil {
		return nil, fmt.Errorf("error parsing files.xml: %w", err)
	}

	fileMapping := make(map[string]File)
	for _, file := range files.Files {
		file.Filename = sanitizeFileName(file.Filename)
		if file.ID == "" || file.ContentHash == "" || file.Filename == "." {
			continue
		}
		fileMapping[file.ID] = file
		fmt.Printf("Added file to mapping: ID=%s, ContentHash=%s, Filename=%s\n", file.ID, file.ContentHash, file.Filename)
	}
	return fileMapping, nil
}

// processActivitiesFolder processes the activities folder and updates the file mapping
// with folder names. It reads folder.xml and inforef.xml files to extract folder names
// and associates them with file IDs.
func processActivitiesFolder(source fs.FS, activitiesFolder string, fileMapping map[string]File) error {
	dirs, err := fs.ReadDir(source, activitiesFolder)
	if err != nil {
		return fmt.Errorf("error reading activities folder: %w", err)
	}

	for _, dir := range dirs {
		if strings.HasPrefix(dir.Name(), "folder_") {
			folderPath := path.Join(activitiesFolder, dir.Name())

			folderXMLPath := path.Join(folderPath, "folder.xml")
			folderFile, err := source.Open(folderXMLPath)
			if err != nil {
				fmt.Printf("Warning: folder.xml not found in %s\n", folderPath)
				continue
			}
			defer folderFile.Close()

			var folderData struct {
				FolderName string `xml:"folder>name"`
			}
			if err := parseXMLFile(folderFile, &folderData); err != nil {
				fmt.Printf("Error parsing folder.xml: %v\n", err)
				continue
			}

			folderName := sanitizeFileName(folderData.FolderName)

			inforefXMLPath := path.Join(folderPath, "inforef.xml")
			inforefFile, err := source.Open(inforefXMLPath)
			if err != nil {
				fmt.Printf("Warning: inforef.xml not found in %s\n", folderPath)
				continue
			}
			defer inforefFile.Close()

			var inforefData struct {
				Files []struct {
					ID string `xml:"id"`
				} `xml:"fileref>file"`
			}
			if err := parseXMLFile(inforefFile, &inforefData); err != nil {
				fmt.Printf("Error parsing inforef.xml: %v\n", err)
				continue
			}

			for _, fileref := range inforefData.Files {
				if file, exists := fileMapping[fileref.ID]; exists {
					file.Folder = folderName
					fileMapping[fileref.ID] = file
					fmt.Printf("Assigned folder to file: ID=%s, Folder=%s\n", fileref.ID, folderName)
				} else {
					fmt.Printf("Warning: File ID %s not found in file_mapping\n", fileref.ID)
				}
			}
		}
	}
	return nil
}

// copyFiles copies files from the source to the destination folder based on the file mapping.
// the file with hash xyz... is in files/xy/xyz...
func copyFiles(source fs.FS, destinationFolder string, fileMapping map[string]File) {
	for _, file := range fileMapping {
		if len(file.ContentHash) < 2 {
			fmt.Printf("Warning: Invalid ContentHash for file ID %s\n", file.ID)
			continue
		}
		// Construct the expected path of the file in the source folder
		sourceFilePath := path.Join("files", file.ContentHash[:2], file.ContentHash)

		// Open the file from the source FS
		sourceFile, err := source.Open(sourceFilePath)
		if err != nil {
			fmt.Printf("Warning: File %s not found in source folder\n", sourceFilePath)
			continue
		}
		defer sourceFile.Close()

		// Construct the destination path
		var destinationPath string
		if file.Folder == "" {
			destinationPath = filepath.Join(destinationFolder, file.Filename)
		} else {
			destinationPath = filepath.Join(destinationFolder, file.Folder, file.Filename)
		}
		// Check if the destination file already exists
		if _, err := os.Stat(destinationPath); err == nil {
			fmt.Printf("Warning: File %s already exists, skipping\n", destinationPath)
			continue
		} else if !os.IsNotExist(err) {
			fmt.Printf("Error checking file %s: %v\n", destinationPath, err)
			continue
		}

		// Ensure the destination directory exists
		destinationDir := filepath.Dir(destinationPath)
		if _, err := os.Stat(destinationDir); os.IsNotExist(err) {
			// Create the directory if it doesn't exist
			fmt.Printf("Creating directory %s\n", destinationDir)
			if err := os.MkdirAll(destinationDir, os.ModePerm); err != nil {
				fmt.Printf("Error creating directory %s: %v\n", destinationDir, err)
				continue
			}
		} else if err != nil {
			fmt.Printf("Error checking directory %s: %v\n", destinationDir, err)
			continue
		}

		// Create the destination file
		destinationFile, err := os.Create(destinationPath)
		if err != nil {
			fmt.Printf("Error creating file %s: %v\n", destinationPath, err)
			continue
		}
		defer destinationFile.Close()

		// Copy the file content
		if _, err := io.Copy(destinationFile, sourceFile); err != nil {
			fmt.Printf("Error copying file %s to %s: %v\n", sourceFilePath, destinationPath, err)
			continue
		}

		fmt.Printf("Moved %s to %s\n", sourceFilePath, destinationPath)
	}
}

type closefn func() error

func targzFS(zipPath string) (fs.FS, closefn, error) {
	// Open the .tar.gz file
	file, err := os.Open(zipPath)
	if err != nil {
		return nil, nil, err
	}

	// Create a gzip reader
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		file.Close()
		return nil, nil, err
	}

	// Create a tar filesystem from the gzip reader
	tarFs, err := tarfs.New(gzReader)
	if err != nil {
		gzReader.Close()
		file.Close()
		return nil, nil, err
	}

	close := func() error {
		errgz := gzReader.Close()
		errf := file.Close()
		return errors.Join(errgz, errf)
	}

	// Return the tar filesystem and a function to close the file
	return tarFs, close, nil
}

func dirFS(dirPath string) (fs.FS, closefn, error) {
	// Use os.DirFS to create a filesystem interface for the directory
	dirFs := os.DirFS(dirPath)

	return dirFs, nil, nil
}

// getSource returns the source filesystem based on the provided path.
// It checks if the path is a directory or a tar.gz file and returns the appropriate fs.FS.
func getSource(sourcePath string) (fs.FS, closefn, error) {

	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, nil, fmt.Errorf("error checking source path: %w", err)
	}
	// check if the source path is a directory
	if info.IsDir() {
		return dirFS(sourcePath)
	}
	// check if it's a .mbz file
	if strings.HasSuffix(sourcePath, ".mbz") {
		return targzFS(sourcePath)
	}

	return nil, nil, fmt.Errorf("only folder and .mbz file are supported: %w", err)
}

func main() {
	// Get the arguments
	if len(os.Args) != 3 {
		fmt.Println("Usage: go run extract.go <extracted_folder> <destination_folder>")
		os.Exit(1)
	}
	sourcePath := os.Args[1]
	destinationFolder := os.Args[2]

	// get the source filesystem
	source, close, err := getSource(sourcePath)
	if err != nil {
		fmt.Printf("Error getting source: %v\n", err)
		os.Exit(1)
	}
	if close != nil {
		defer func() {
			if err := close(); err != nil {
				fmt.Printf("Error closing source: %v\n", err)
			}
		}()
	}

	// find all the files in the source
	fileMapping, err := buildFileMapping(source, "files.xml")
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	// assign folder names to the files
	if err := processActivitiesFolder(source, "activities", fileMapping); err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	// copy the files to the destination folder
	copyFiles(source, destinationFolder, fileMapping)

	// this is the end
	fmt.Println("Déplacement des fichiers terminé.")
}
