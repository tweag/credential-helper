package lockfile

import "os"

type Lockfile struct {
	file *os.File
}

func New(path string) (Lockfile, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		return Lockfile{}, err
	}
	if err := lock(file); err != nil {
		return Lockfile{}, err
	}

	return Lockfile{file: file}, nil
}

func (l Lockfile) Close() error {
	// close might fail,
	// but for our purposes it's fine to ignore the error
	defer l.file.Close()

	if err := unlock(l.file); err != nil {
		return err
	}

	return nil
}
