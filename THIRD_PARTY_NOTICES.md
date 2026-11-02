# Third-party notices

This product includes software developed by quickfixengine.org (http://www.quickfixengine.org/).

QuickFIX/Go v0.9.12 is used as an unmodified dependency. The FIX44.xml dictionary in spec/ is copied from that version. Its QuickFIX Software License 1.0 is retained in third_party/QUICKFIX_LICENSE. Redistribution must retain its notices, acknowledgment and naming restrictions.

The unmodified linked runtime dependencies and their upstream license texts are:

| Component | Version | License text |
|---|---|---|
| QuickFIX/Go | v0.9.12 | [QuickFIX Software License 1.0](third_party/QUICKFIX_LICENSE) |
| go-proxyproto | v0.7.0 | [Apache 2.0](third_party/PROXYPROTO_LICENSE) |
| pkg/errors | v0.9.1 | [BSD 2-Clause](third_party/PKG_ERRORS_LICENSE) |
| udecimal | v1.8.0 | [BSD 3-Clause](third_party/UDECIMAL_LICENSE) |
| shopspring/decimal | v1.4.0 | [MIT](third_party/DECIMAL_LICENSE) |
| golang.org/x/net | v0.24.0 | [BSD 3-Clause](third_party/X_NET_LICENSE) |
| gopkg.in/yaml.v3 | v3.0.1 | [MIT/Apache terms by file](third_party/YAML_LICENSE) |
| Go runtime/standard library | 1.27.3 (verified build) | [BSD 3-Clause](third_party/GO_LICENSE) |

The inventory generator reads the actual Go executable's linked-module metadata and SHA256; see `scripts/sbom`. It produces a Go module inventory, not an SPDX/CycloneDX document or an inventory of operating-system image packages. Go test/build dependencies are recorded in go.mod/go.sum but are not necessarily linked into the daemon. No vulnerability audit is implied.

The separate test-only QuickFIX/J fixture uses QuickFIX/J 2.3.2 under the QuickFIX Software License 1.0. Its Maven dependency graph is separate from the daemon inventory and specified in interop/quickfixj/pom.xml. Review those artifacts' upstream notices before distributing the test image. Java/Maven and container base images have their own terms; they are not daemon Go dependencies. No code from other prior-art projects has been copied. Project licensing remains undecided; dependency licenses do not grant a license for this project's own code.
