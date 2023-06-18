.PHONY: update
update:
	hack/update-all.sh

.PHONY: update-codegen
update-codegen:
	hack/update-codegen.sh

.PHONY: update-vendor
update-vendor:
	go mod tidy && go mod vendor