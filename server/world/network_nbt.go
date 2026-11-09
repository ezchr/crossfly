package world

// NetworkNBT returns block entity data for Bedrock clients. A block may keep server-only saved state without
// inventing a client block entity by implementing EncodeNetworkNBT and returning nil.
func NetworkNBT(b NBTer) map[string]any {
	if network, ok := b.(interface{ EncodeNetworkNBT() map[string]any }); ok {
		return network.EncodeNetworkNBT()
	}
	return b.EncodeNBT()
}
