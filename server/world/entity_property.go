package world

// EntityPropertyType is the value type of an EntityProperty.
type EntityPropertyType uint8

// The EntityPropertyTypes, numbered as Bedrock numbers them on the network.
const (
	EntityPropertyInt EntityPropertyType = iota
	EntityPropertyFloat
	EntityPropertyBool
	EntityPropertyEnum
)

// EntityProperty describes one entry of "description.properties" in a
// behaviour pack entity definition, such as minecraft:climate_variant of a
// cow. An EntityType with properties lists them through an
// EntityProperties() []EntityProperty method; viewers use the list to tell
// clients about the properties and to encode the values an entity reports.
type EntityProperty struct {
	// Name is the namespaced name of the property, such as
	// "minecraft:climate_variant".
	Name string
	// Type is the value type of the property.
	Type EntityPropertyType
	// Min and Max are the range of an int or float property.
	Min, Max float64
	// Enum holds the values of an enum property in definition order.
	Enum []string
	// ClientSync is true if clients are told about the property and its value.
	ClientSync bool
	// Default is the default value if it is a constant: an int, float64,
	// bool, or for an enum the value's name. It is nil if the default is a
	// Molang expression.
	Default any
}
