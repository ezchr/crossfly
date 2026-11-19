package sound

// WindChargeThrow is the sound of a player throwing a wind charge. Bedrock plays the generic "throw"
// (ThrowableItemComponent::_doThrow, LevelSoundEvent 40); Java plays entity.wind_charge.throw.
type WindChargeThrow struct{ sound }

// WindChargeBurst is the sound of a wind charge bursting (wind_charge.burst, the sound_effect of the
// minecraft:explode component of wind_charge_projectile).
type WindChargeBurst struct{ sound }

// CandleExtinguish is the sound of candles going out (AbstractCandleBlock::_extinguish, extinguish.candle).
type CandleExtinguish struct{ sound }
