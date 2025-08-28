## Unreleased
- enforce non-empty instance names with length checks (migration `002_instance_name_required`)
- Fixed 404 on resync. /resync temporarily aliased to /sync.
- Allow resyncing an instance without a request body when it already has a PufferPanel server ID.
