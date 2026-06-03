// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

// SupportsInput reports whether model metadata allows a request content kind.
// Models with no explicit input list are treated as text-only for backwards
// compatibility with earlier metadata.
func (model Model) SupportsInput(kind ContentBlockType) bool {
	if kind == "" {
		return false
	}
	if len(model.SupportedInputs) == 0 {
		return kind == ContentBlockText
	}
	for _, supported := range model.SupportedInputs {
		if supported == kind {
			return true
		}
	}
	return false
}

// SupportsReasoning reports whether model metadata advertises provider
// reasoning or thinking support.
func (model Model) SupportsReasoning() bool {
	return model.SupportsThinking ||
		len(model.ThinkingLevels) > 0 ||
		len(model.ThinkingLevelMap) > 0
}

// SupportsThinkingLevel reports whether level can be requested for model.
func (model Model) SupportsThinkingLevel(level ThinkingLevel) bool {
	if level == "" {
		return false
	}
	if model.unsupportedThinkingLevel(level) {
		return false
	}
	if level == ThinkingLevelOff {
		return true
	}
	if len(model.ThinkingLevelMap) > 0 {
		_, ok := model.ThinkingLevelMap[level]
		return ok
	}
	if len(model.ThinkingLevels) > 0 {
		for _, supported := range model.ThinkingLevels {
			if supported == level {
				return true
			}
		}
		return false
	}
	return model.SupportsThinking
}

func (model Model) unsupportedThinkingLevel(level ThinkingLevel) bool {
	for _, unsupported := range model.UnsupportedThinkingLevels {
		if unsupported == level {
			return true
		}
	}
	return false
}

// ProviderThinkingLevel returns the provider-specific value for level.
// If a model only lists supported levels, the provider value is the level text.
func (model Model) ProviderThinkingLevel(level ThinkingLevel) (string, bool) {
	if !model.SupportsThinkingLevel(level) {
		return "", false
	}
	if level == ThinkingLevelOff {
		return "", true
	}
	if len(model.ThinkingLevelMap) > 0 {
		return model.ThinkingLevelMap[level], true
	}
	return string(level), true
}

// SupportsImages reports whether model accepts image content as input.
func (model Model) SupportsImages() bool {
	return model.SupportsInput(ContentBlockImage)
}

// CacheEnabled reports whether this retention requests provider-side prompt
// caching. Empty retention and CacheRetentionNone both mean no cache.
func (retention CacheRetention) CacheEnabled() bool {
	return retention != "" && retention != CacheRetentionNone
}

// CacheShortLived reports whether this retention asks for a short-lived prompt
// cache entry.
func (retention CacheRetention) CacheShortLived() bool {
	return retention == CacheRetentionShort || retention == CacheRetentionEphemeral
}

// CacheLongLived reports whether this retention asks for a long-lived prompt
// cache entry.
func (retention CacheRetention) CacheLongLived() bool {
	return retention == CacheRetentionLong || retention == CacheRetentionPersistent
}
