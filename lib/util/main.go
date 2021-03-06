package util

import (
	"fmt"
	"os"
	"testing"
)

////////////////////////////////////////////////////////
//                                                    //
//               Test Helper Functions                //
//                                                    //
////////////////////////////////////////////////////////

func BuildExamplePackagePath(t *testing.T, packageName string, abs bool) string {
	t.Helper()
	gopath := os.Getenv("GOPATH")
	if abs {
		return fmt.Sprintf("%s/src/gitlab.com/verygoodsoftwarenotvirus/blanket/example_packages/%s", gopath, packageName)
	}
	return fmt.Sprintf("gitlab.com/verygoodsoftwarenotvirus/blanket/example_packages/%s", packageName)
}

func BuildExampleFilePath(filename string) string {
	gopath := os.Getenv("GOPATH")
	return fmt.Sprintf("%s/src/gitlab.com/verygoodsoftwarenotvirus/blanket/example_files/%s", gopath, filename)
}
