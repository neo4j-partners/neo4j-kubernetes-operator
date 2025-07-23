/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package integration_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
)

var _ = Describe("Version Detection Integration Tests", func() {
	Context("When detecting Neo4j versions", func() {
		It("Should parse SemVer versions correctly", func() {
			testCases := []struct {
				image         string
				expectedMajor int
				expectedMinor int
				expectedPatch int
				isCalver      bool
			}{
				{"neo4j:5.26.0", 5, 26, 0, false},
				{"neo4j:5.26.0-enterprise", 5, 26, 0, false},
				{"neo4j:5.27.1", 5, 27, 1, false},
				{"neo4j:5.28.0-community", 5, 28, 0, false},
			}

			for _, tc := range testCases {
				By(fmt.Sprintf("Parsing version from image %s", tc.image))
				version, err := neo4j.GetImageVersion(tc.image)
				Expect(err).NotTo(HaveOccurred())
				Expect(version).NotTo(BeNil())
				Expect(version.IsCalver).To(Equal(tc.isCalver))
				Expect(version.Major).To(Equal(tc.expectedMajor))
				Expect(version.Minor).To(Equal(tc.expectedMinor))
				Expect(version.Patch).To(Equal(tc.expectedPatch))
			}
		})

		It("Should parse CalVer versions correctly", func() {
			testCases := []struct {
				image         string
				expectedMajor int
				expectedMinor int
				expectedPatch int
				isCalver      bool
			}{
				{"neo4j:2025.01.0", 2025, 1, 0, true},
				{"neo4j:2025.01.0-enterprise", 2025, 1, 0, true},
				{"neo4j:2025.02.1", 2025, 2, 1, true},
				{"neo4j:2025.06.0-community", 2025, 6, 0, true},
			}

			for _, tc := range testCases {
				By(fmt.Sprintf("Parsing version from image %s", tc.image))
				version, err := neo4j.GetImageVersion(tc.image)
				Expect(err).NotTo(HaveOccurred())
				Expect(version).NotTo(BeNil())
				Expect(version.IsCalver).To(Equal(tc.isCalver))
				Expect(version.Major).To(Equal(tc.expectedMajor))
				Expect(version.Minor).To(Equal(tc.expectedMinor))
				Expect(version.Patch).To(Equal(tc.expectedPatch))
			}
		})

		It("Should generate correct backup commands for different versions", func() {
			By("Testing Neo4j 5.26.x backup command")
			version526, err := neo4j.GetImageVersion("neo4j:5.26.0")
			Expect(err).NotTo(HaveOccurred())

			backupCmd := neo4j.GetBackupCommand(version526, "mydb", "/backups/mydb", false)
			Expect(backupCmd).To(Equal("neo4j-admin database backup mydb --to-path=/backups/mydb"))

			By("Testing Neo4j 2025.x backup command")
			version2025, err := neo4j.GetImageVersion("neo4j:2025.01.0")
			Expect(err).NotTo(HaveOccurred())

			backupCmd2025 := neo4j.GetBackupCommand(version2025, "mydb", "/backups/mydb", false)
			Expect(backupCmd2025).To(Equal("neo4j-admin database backup mydb --to-path=/backups/mydb"))

			By("Testing backup all databases")
			backupAllCmd := neo4j.GetBackupCommand(version526, "", "/backups/all", true)
			Expect(backupAllCmd).To(Equal("neo4j-admin database backup --include-metadata=all --to-path=/backups/all"))
		})

		It("Should generate correct restore commands for different versions", func() {
			By("Testing Neo4j 5.26.x restore command")
			version526, err := neo4j.GetImageVersion("neo4j:5.26.0")
			Expect(err).NotTo(HaveOccurred())

			restoreCmd := neo4j.GetRestoreCommand(version526, "mydb", "/backups/mydb")
			Expect(restoreCmd).To(Equal("neo4j-admin database restore --from-path=/backups/mydb mydb"))

			By("Testing Neo4j 2025.x restore command")
			version2025, err := neo4j.GetImageVersion("neo4j:2025.01.0")
			Expect(err).NotTo(HaveOccurred())

			restoreCmd2025 := neo4j.GetRestoreCommand(version2025, "mydb", "/backups/mydb")
			Expect(restoreCmd2025).To(Equal("neo4j-admin database restore --from-path=/backups/mydb mydb"))
		})

		It("Should identify version support correctly", func() {
			By("Testing supported versions")
			version526, _ := neo4j.GetImageVersion("neo4j:5.26.0")
			Expect(version526.IsSupported()).To(BeTrue())

			version527, _ := neo4j.GetImageVersion("neo4j:5.27.0")
			Expect(version527.IsSupported()).To(BeTrue())

			version2025, _ := neo4j.GetImageVersion("neo4j:2025.01.0")
			Expect(version2025.IsSupported()).To(BeTrue())

			By("Testing unsupported versions")
			version525, _ := neo4j.GetImageVersion("neo4j:5.25.0")
			Expect(version525.IsSupported()).To(BeFalse())

			version4, _ := neo4j.GetImageVersion("neo4j:4.4.0")
			Expect(version4.IsSupported()).To(BeFalse())
		})

		It("Should handle version comparison correctly", func() {
			By("Comparing SemVer versions")
			v526, _ := neo4j.GetImageVersion("neo4j:5.26.0")
			v527, _ := neo4j.GetImageVersion("neo4j:5.27.0")
			Expect(v526.Compare(v527)).To(Equal(-1))
			Expect(v527.Compare(v526)).To(Equal(1))
			Expect(v526.Compare(v526)).To(Equal(0))

			By("Comparing CalVer versions")
			v2025_01, _ := neo4j.GetImageVersion("neo4j:2025.01.0")
			v2025_02, _ := neo4j.GetImageVersion("neo4j:2025.02.0")
			Expect(v2025_01.Compare(v2025_02)).To(Equal(-1))
			Expect(v2025_02.Compare(v2025_01)).To(Equal(1))

			By("Comparing SemVer vs CalVer")
			// CalVer should be considered newer than SemVer
			Expect(v526.Compare(v2025_01)).To(Equal(-1))
			Expect(v2025_01.Compare(v526)).To(Equal(1))
		})

		It("Should detect Cypher language version support", func() {
			By("Testing Neo4j 5.26.x - no Cypher language version support")
			version526, _ := neo4j.GetImageVersion("neo4j:5.26.0")
			Expect(version526.SupportsCypherLanguageVersion()).To(BeFalse())

			By("Testing Neo4j 2025.x - supports Cypher language version")
			version2025, _ := neo4j.GetImageVersion("neo4j:2025.01.0")
			Expect(version2025.SupportsCypherLanguageVersion()).To(BeTrue())
		})

		It("Should handle edge cases in version parsing", func() {
			By("Testing invalid image formats")
			_, err := neo4j.GetImageVersion("neo4j")
			Expect(err).To(HaveOccurred())

			_, err = neo4j.GetImageVersion("neo4j:latest")
			Expect(err).To(HaveOccurred())

			_, err = neo4j.GetImageVersion("neo4j:invalid-version")
			Expect(err).To(HaveOccurred())

			By("Testing custom registries")
			version, err := neo4j.GetImageVersion("my-registry.com/neo4j:5.26.0-enterprise")
			Expect(err).NotTo(HaveOccurred())
			Expect(version.Major).To(Equal(5))
			Expect(version.Minor).To(Equal(26))

			By("Testing version with multiple hyphens")
			version, err = neo4j.GetImageVersion("neo4j:5.26.0-enterprise-aura")
			Expect(err).NotTo(HaveOccurred())
			Expect(version.Major).To(Equal(5))
			Expect(version.Minor).To(Equal(26))
		})
	})
})
