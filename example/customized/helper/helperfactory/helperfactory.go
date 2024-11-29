package helperfactory

import (
	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/example/customized/helper/authenticate"
)

func CustomHelperFactory(_ string) (api.Helper, error) {
	return authenticate.PathToHeader{}, nil
}
