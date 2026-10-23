package model_accessor_result

import m "foundry.test/goway/models/mutators"

func invalid(v m.Member) int { result, _ := v.AccessEmail(); return result }
