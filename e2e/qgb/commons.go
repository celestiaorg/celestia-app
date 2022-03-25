package e2e

import (
	"fmt"
	"github.com/google/uuid"
	tc "github.com/testcontainers/testcontainers-go"
	"strings"
)

var ComposeFilePaths = []string{"./docker-compose.yml"}

func StartCluster() (identifier string, err error) {
	identifier = strings.ToLower(uuid.New().String())

	compose := tc.NewLocalDockerCompose(ComposeFilePaths, identifier)
	execError := compose.
		WithCommand([]string{"up", "-d"}).
		WithEnv(map[string]string{
			"key1": "value1",
			"key2": "value2",
		}).
		Invoke()
	err = execError.Error
	if err != nil {
		err = fmt.Errorf("Could not run compose file: %v - %v", ComposeFilePaths, err)
	}
	return identifier, err
}
