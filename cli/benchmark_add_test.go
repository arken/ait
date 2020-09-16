package cli

import (
	"fmt"
	"github.com/DataDrake/cli-ng/cmd"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBigAdd(t *testing.T) {
	u, _ := os.UserHomeDir()
	testRoot := filepath.Join(u, "Documents/")
	_ = os.Chdir(testRoot)
	_ = os.RemoveAll(testRoot + "/.ait")
	InitRun(nil, nil)
	addArgs := &cmd.CMD{
		Args: &AddArgs{Paths: []string{"."}},
	}
	start := time.Now()
	AddRun(nil, addArgs)
	fmt.Println("\n\t******** Add all took", time.Since(start).Milliseconds(), "ms ********\n ")
	_ = os.RemoveAll(testRoot + "/.ait")
}

func TestAddManyDirs(t *testing.T) {
	u, _ := os.UserHomeDir()
	testRoot := filepath.Join(u, "Documents/")
	_ = os.Chdir(testRoot)
	_ = os.RemoveAll(testRoot + "/.ait")
	InitRun(nil, nil)
	files, _ := ioutil.ReadDir(testRoot)
	var fileNames []string
	for _, fi := range files {
		fileNames = append(fileNames, fi.Name())
	}
	addArgs := &cmd.CMD{
		Args: &AddArgs{Paths: fileNames},
	}
	start := time.Now()
	AddRun(nil, addArgs)
	fmt.Println("\n\t******** Add dirs took", time.Since(start).Milliseconds(), "ms ********\n ")
	_ = os.RemoveAll(testRoot + "/.ait")
}

func TestAddManyFiles(t *testing.T) {
	u, _ := os.UserHomeDir()
	testRoot := filepath.Join(u, "Documents/")
	_ = os.Chdir(testRoot)
	_ = os.RemoveAll(testRoot + "/.ait")
	InitRun(nil, nil)
	addArgs := &cmd.CMD{
		Args: &AddArgs{Paths: []string{
			//put the paths to a bunch of individual files in here for testing
		}},
	}
	start := time.Now()
	AddRun(nil, addArgs)
	fmt.Println("\n\t******** Add files took", time.Since(start).Milliseconds(), "ms ********\n ")
	_ = os.RemoveAll(testRoot + "/.ait")
}

//func TestUnicode(t *testing.T) {
//	file, err := os.Create("日本語")
//	utils.CheckError(err)
//	info, _ := os.Stat(file.Name())
//	s := info.Name()
//	_ = os.Remove(file.Name())
//	fmt.Println(s)
//	file = utils.BasicFileOpen("utf8test",
//		os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
//	m := map[string]struct{}{ s: {} }
//	err = utils.DumpMap(m, file)
//	utils.CheckError(err)
//	//remember to delete utf8test after checking to make sure s was printed
//	//right and isn't a bunch of gibberish.
//}