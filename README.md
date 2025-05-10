# Moodle File Extractor

Moodle File Extractor (MFE) is a command-line tool designed to extract all files from a `.mbz` Moodle backup file or an extracted folder.

## Usage
```bash
mfe <source> <destination_folder>
```

### Arguments
- `<source>`: Path to the `.mbz` file or a folder containing the extracted `.mbz` file.
- `<destination_folder>`: Path to the destination folder where files will be stored.

### Options
- `-d`, `--debug`: Enable debug mode for detailed logging.

### Example
```bash
mfe backup.mbz moodle_files/
```

## Installation

### Download binary
You can download the latest binary release from the [Releases](https://github.com/ktzanev/mfe/releases) page.

### Build from source

Use go to build the binary:
```bash
go install github.com/ktzanev/mfe@latest
```

## How it Works
The .mbz file is a .tar.gz archive with the following structure:

```
folders :
  activities
  course
  files
  sections
files : 
  .ARCHIVE_INDEX
  completion.xml
  files.xml
  grade_history.xml
  gradebook.xml
  groups.xml
  moodle_backup.log
  moodle_backup.xml
  outcomes.xml
  questions.xml
  roles.xml
  scales.xml
```

1. The tool reads the `files.xml` file to map file IDs to their respective files. 
2. For all folders in `activities` folder that has a name starting with `folder_`, it processes the `folder.xml` and `inforef.xml` files to get the folder structure.
3. It then copies the files that are in the `files` folder to the destination folder, maintaining the folder structure.

## License

[MIT License](LICENSE)
