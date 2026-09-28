package invalid

import "foundry.test/consumer/localization"

func invalid(key string) { _ = localization.WelcomeArgsValidationFields().Name.WithLabelKey(key) }
