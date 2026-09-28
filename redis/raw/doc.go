// Package raw is the explicit trusted Redis command boundary. Normal application
// features use Foundry's typed cache, data, lease and pub/sub APIs. Commands here
// require scoped keys and typed decoders, but arbitrary arguments and Lua source
// are trusted code: Foundry cannot prove that they only refer to declared keys.
package raw
