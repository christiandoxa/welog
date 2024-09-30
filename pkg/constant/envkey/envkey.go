// Package envkey defines environment variable keys used for configuring the application's
// connection to ElasticSearch. These keys are used to retrieve configuration values
// from environment variables, ensuring that sensitive data and configuration details
// are not hardcoded within the application.
package envkey

// ElasticIndex is the environment variable key used to specify the index name for ElasticSearch.
// This index is used to store logs and other structured data within the ElasticSearch cluster.
const ElasticIndex = "ELASTIC_INDEX__"

// ElasticPassword is the environment variable key used to specify the password for authenticating
// with ElasticSearch. This password, together with the username, secures the connection to ElasticSearch.
const ElasticPassword = "ELASTIC_PASSWORD__"

// ElasticURL is the environment variable key used to specify the URL of the ElasticSearch instance.
// This URL is required to connect the application to the ElasticSearch service for logging and data storage.
const ElasticURL = "ELASTIC_URL__"

// ElasticUsername is the environment variable key used to specify the username for authenticating
// with ElasticSearch. This username, in combination with the password, provides secure access to ElasticSearch.
const ElasticUsername = "ELASTIC_USERNAME__"
