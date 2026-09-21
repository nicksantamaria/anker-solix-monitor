# Changelog

## [1.5.1](https://github.com/nicksantamaria/anker-solix-monitor/compare/v1.5.0...v1.5.1) (2026-09-21)


### Bug Fixes

* **api:** render dashboard charts in two columns on landscape tablets ([#35](https://github.com/nicksantamaria/anker-solix-monitor/issues/35)) ([a72d836](https://github.com/nicksantamaria/anker-solix-monitor/commit/a72d8360378e5438a7eff1319b8b6b212bf626a6))

## [1.5.0](https://github.com/nicksantamaria/anker-solix-monitor/compare/v1.4.1...v1.5.0) (2026-09-21)


### Features

* add BLE command sending and solix-cli set subcommands ([#17](https://github.com/nicksantamaria/anker-solix-monitor/issues/17)) ([1f3c962](https://github.com/nicksantamaria/anker-solix-monitor/commit/1f3c962a3f132e52b6b7f0e098f27eea3077d659))
* **api:** add generated apple touch icon asset ([#34](https://github.com/nicksantamaria/anker-solix-monitor/issues/34)) ([b6d8886](https://github.com/nicksantamaria/anker-solix-monitor/commit/b6d888691d8719c5ba13d40db8e830b6f049203c))


### Bug Fixes

* **api:** use 4-card grid on iPad landscape ([#32](https://github.com/nicksantamaria/anker-solix-monitor/issues/32)) ([be384c9](https://github.com/nicksantamaria/anker-solix-monitor/commit/be384c9986c0f70b8652a8d94c52158382132e3b))


### Performance Improvements

* **api:** remove low-value dashboard charts and slow UI polling to 5m ([#31](https://github.com/nicksantamaria/anker-solix-monitor/issues/31)) ([ee9a4ee](https://github.com/nicksantamaria/anker-solix-monitor/commit/ee9a4ee96e2d784c5e9269184a17541ef5f0543e))

## [1.4.1](https://github.com/nicksantamaria/anker-solix-monitor/compare/v1.4.0...v1.4.1) (2026-09-21)


### Bug Fixes

* **api:** improve dashboard chart compatibility for legacy browsers ([#29](https://github.com/nicksantamaria/anker-solix-monitor/issues/29)) ([7085a2d](https://github.com/nicksantamaria/anker-solix-monitor/commit/7085a2d7cc31b9acdb6611764a15a35a88b81b2d))

## [1.4.0](https://github.com/nicksantamaria/anker-solix-monitor/compare/v1.3.0...v1.4.0) (2026-09-21)


### Features

* Add Solix-themed favicon to dashboard tab ([#24](https://github.com/nicksantamaria/anker-solix-monitor/issues/24)) ([9b299d7](https://github.com/nicksantamaria/anker-solix-monitor/commit/9b299d7d4c9a6a97e01c3b15fd990e0f8c40e282))
* Reduce /api/history payload size with server-side bucketing and restore full-range chart coverage ([#26](https://github.com/nicksantamaria/anker-solix-monitor/issues/26)) ([5f9e20a](https://github.com/nicksantamaria/anker-solix-monitor/commit/5f9e20af5808d9714f24cdd350c283c4c6e4ccde))

## [1.3.0](https://github.com/nicksantamaria/anker-solix-monitor/compare/v1.2.1...v1.3.0) (2026-09-21)


### Features

* Add 60s API caching and slim history payload for dashboard endpoints ([#23](https://github.com/nicksantamaria/anker-solix-monitor/issues/23)) ([02b4cf3](https://github.com/nicksantamaria/anker-solix-monitor/commit/02b4cf356f65c88a66164715debcf81bd4d10af5))


### Bug Fixes

* fix dashboard JavaScript compatibility for iOS 9.3.5 ([#21](https://github.com/nicksantamaria/anker-solix-monitor/issues/21)) ([ec495e7](https://github.com/nicksantamaria/anker-solix-monitor/commit/ec495e76a12eaa44cc18d21d47d6835d2e299ae4))

## [1.2.1](https://github.com/nicksantamaria/anker-solix-monitor/compare/v1.2.0...v1.2.1) (2026-08-13)


### Bug Fixes

* F2000 periodic re-query and add poll log messages ([#19](https://github.com/nicksantamaria/anker-solix-monitor/issues/19)) ([4a37154](https://github.com/nicksantamaria/anker-solix-monitor/commit/4a37154412aefcea970b85e67ddf191ff2a4e616))

## [1.2.0](https://github.com/nicksantamaria/anker-solix-monitor/compare/v1.1.0...v1.2.0) (2026-08-12)


### Features

* add drag-and-drop panel reordering with localStorage persistence ([#13](https://github.com/nicksantamaria/anker-solix-monitor/issues/13)) ([829bd49](https://github.com/nicksantamaria/anker-solix-monitor/commit/829bd49ea192de05c481fcef6119872025b2109f))


### Bug Fixes

* cancel stale in-flight chart fetches with AbortController ([#15](https://github.com/nicksantamaria/anker-solix-monitor/issues/15)) ([847487e](https://github.com/nicksantamaria/anker-solix-monitor/commit/847487eb25ab4ea043fc1ed00cb1e47c9889d73a))

## [1.1.0](https://github.com/nicksantamaria/anker-solix-monitor/compare/v1.0.1...v1.1.0) (2026-08-12)


### Features

* add linux/arm GOARM=6 build permutation ([#10](https://github.com/nicksantamaria/anker-solix-monitor/issues/10)) ([984d575](https://github.com/nicksantamaria/anker-solix-monitor/commit/984d5752e27c81c30485a453e0864335c112108b))


### Bug Fixes

* BLE notification fragmentation on Linux (Raspberry Pi) ([#9](https://github.com/nicksantamaria/anker-solix-monitor/issues/9)) ([1ff646e](https://github.com/nicksantamaria/anker-solix-monitor/commit/1ff646e4d23e873193cbd59fd289e53c3349f1d7))

## [1.0.1](https://github.com/nicksantamaria/anker-solix-monitor/compare/v1.0.0...v1.0.1) (2026-08-12)


### Bug Fixes

* use macos runner for darwin builds in release workflow ([#7](https://github.com/nicksantamaria/anker-solix-monitor/issues/7)) ([8b8bd73](https://github.com/nicksantamaria/anker-solix-monitor/commit/8b8bd732afe00925a0df73ed061bcc5ad84061c4))

## 1.0.0 (2026-08-12)


### Features

* add solix-cli — scan, status, and monitor subcommands ([#2](https://github.com/nicksantamaria/anker-solix-monitor/issues/2)) ([57a4944](https://github.com/nicksantamaria/anker-solix-monitor/commit/57a49441ef273011e4c879bbaaf8184a3a8a8201))
* self-hosted Renovate, Release Please, and static release binaries ([#5](https://github.com/nicksantamaria/anker-solix-monitor/issues/5)) ([36c58ec](https://github.com/nicksantamaria/anker-solix-monitor/commit/36c58ecca58a8555e599474fc3183509eef54e49))
