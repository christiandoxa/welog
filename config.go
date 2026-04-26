package welog

// Config contains the Elasticsearch connection settings used by Welog.
type Config struct {
	ElasticIndex    string
	ElasticURL      string
	ElasticUsername string
	ElasticPassword string
}
