package tools

import "go.mongodb.org/mongo-driver/v2/mongo/options"

// mongoFindOpts is a tiny helper to keep the v2 driver options call site
// readable from mongo_tools.go.
func mongoFindOpts(limit int64) options.Lister[options.FindOptions] {
	return options.Find().SetLimit(limit)
}
