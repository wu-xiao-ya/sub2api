package repository

// Each rule is directional: pinned official file <- verified deployed custom SQL.
// No migration file or schema_migrations row is changed. Review: ops/refork/README.md.
var reforkMigrationChecksumCompatibilityRules = map[string]migrationChecksumCompatibilityRule{
	"195_channel_monitor_mode.sql": newReforkMigrationChecksumCompatibilityRule(
		"73c39ac374c722253135041466108836845828a6065b499c60e7f27d6b92c21c",
		"1fdd9baa85b13205688a23ae5d3a019f98572e58a197ccb28ae3b93807d5d1de"),
	"203_channel_monitor_v2_default_ignore_and_cache.sql": newReforkMigrationChecksumCompatibilityRule(
		"ae54ca5d660438136f559cf2b65dae15399281de195a19159d03ec596de4bb98",
		"2481dcd6e4d148d81a16cb0b31d3b5996e54d0ad1e9ab35a5150c34ffa228873"),
	"204_channel_monitor_hide_throughput.sql": newReforkMigrationChecksumCompatibilityRule(
		"772dc9127edd5af6eefd25b12c7b23752c0701537c4d8815ba3672c1ad65c3ba",
		"d57313df796f5e33a87d4bf82687c6744e7e920b32a121b629c3f09fbdad1e59"),
	"218_group_audio_voice_pricing.sql": newReforkMigrationChecksumCompatibilityRule(
		"a99ade7d0d464c67bf56814570050cc363ffad64eae2cb1e1ed760065f0b3585",
		"343a955e52348ce92c35753e78ca3f8e5a76060c20af71061ca5e04c6ed84085"),
	"219_group_search_price_per_1k.sql": newReforkMigrationChecksumCompatibilityRule(
		"430c2e3595342fe22c59e9676e9b18ea376f076324b77174a21e6f181f57f4b5",
		"833578274d0eed24d39355298d5659b33e5484c869b331ffd815187c221552d2"),
}

func newReforkMigrationChecksumCompatibilityRule(fileChecksum, dbChecksum string) migrationChecksumCompatibilityRule {
	return newMigrationChecksumCompatibilityRule(fileChecksum, dbChecksum)
}

func isReforkMigrationChecksumCompatible(name, dbChecksum, fileChecksum string) bool {
	rule, ok := reforkMigrationChecksumCompatibilityRules[name]
	if !ok || fileChecksum != rule.fileChecksum {
		return false
	}
	_, ok = rule.acceptedDBChecksum[dbChecksum]
	return ok
}
