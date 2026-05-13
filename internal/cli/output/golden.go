package output

import "flag"

var updateGoldens = flag.Bool("update", false, "update golden test fixtures")

func UpdateGoldens() bool {
	return updateGoldens != nil && *updateGoldens
}
