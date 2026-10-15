"""Exercise version/tag scripts in disposable repositories, never the checkout."""
import pathlib
import shutil
import subprocess
import tempfile
import unittest

SOURCE = pathlib.Path(__file__).resolve().parents[1]

class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = pathlib.Path(self.directory.name) / 'work'
        self.root.mkdir()
        (self.root / 'bin').mkdir()
        for name in ('bump-version', 'release'):
            shutil.copy2(SOURCE / 'bin' / name, self.root / 'bin' / name)
        (self.root / 'VERSION').write_text('0.1.0\n')
        self.run_cmd('git', 'init', '-q')
        self.run_cmd('git', 'config', 'user.name', 'Release test')
        self.run_cmd('git', 'config', 'user.email', 'release@example.invalid')
        self.run_cmd('git', 'add', '.')
        self.run_cmd('git', 'commit', '-qm', 'Initial fixture')
        remote = pathlib.Path(self.directory.name) / 'origin.git'
        self.run_cmd('git', 'init', '--bare', '-q', str(remote))
        self.run_cmd('git', 'remote', 'add', 'origin', str(remote))

    def run_cmd(self, *args, success=True):
        result = subprocess.run(args, cwd=self.root, text=True, capture_output=True)
        if success:
            self.assertEqual(result.returncode, 0, result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0)
        return result.stdout.strip()

    def test_semantic_bumps(self):
        for part, expected in [('patch', '0.1.1'), ('minor', '0.2.0'), ('major', '1.0.0')]:
            self.run_cmd('bash', 'bin/bump-version', part)
            self.assertEqual((self.root / 'VERSION').read_text().strip(), expected)
        self.run_cmd('bash', 'bin/bump-version', 'invalid', success=False)
        self.assertEqual((self.root / 'VERSION').read_text().strip(), '1.0.0')

    def test_release_requires_committed_version_and_unique_tag(self):
        self.run_cmd('bash', 'bin/release', '--dry-run')
        self.assertEqual(self.run_cmd('git', 'tag'), '')
        self.run_cmd('bash', 'bin/bump-version', 'patch')
        self.run_cmd('bash', 'bin/release', success=False)
        self.run_cmd('git', 'add', 'VERSION')
        self.run_cmd('git', 'commit', '-qm', 'Bump version')
        self.run_cmd('bash', 'bin/release')
        self.assertEqual(self.run_cmd('git', 'tag'), 'v0.1.1')
        self.run_cmd('bash', 'bin/release', success=False)
        self.run_cmd('git', 'push', '-q', 'origin', 'v0.1.1')
        self.run_cmd('git', 'tag', '-d', 'v0.1.1')
        self.run_cmd('bash', 'bin/release', success=False)

    def test_remote_failure_cannot_create_tag(self):
        self.run_cmd('git', 'remote', 'set-url', 'origin', str(self.root / 'missing.git'))
        self.run_cmd('bash', 'bin/release', success=False)
        self.assertEqual(self.run_cmd('git', 'tag'), '')

if __name__ == '__main__':
    unittest.main()
