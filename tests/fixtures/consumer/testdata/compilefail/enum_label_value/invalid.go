package invalid

import "foundry.test/consumer/localization"

func invalid(value string) { _, _ = localization.Ready.EnumDescriptor().LabelKey(value) }
