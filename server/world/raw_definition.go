package world

// RawDefinitionBlock is a CustomBlock that sends its client definition (the block properties of
// StartGame) as it is, instead of one built from Properties: a block that must look exactly like
// another server's (a proxy merges the two servers' definitions).
type RawDefinitionBlock interface {
	CustomBlock
	RawDefinition() map[string]any
}

// RawDefinitionItem is a CustomItem that sends its item registry entry's data as it is, for the
// same reason.
type RawDefinitionItem interface {
	CustomItem
	RawDefinition() (componentBased bool, version int32, data map[string]any)
}
