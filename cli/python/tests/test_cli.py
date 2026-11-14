from pathlib import Path
import tempfile
import unittest
import zipfile
from hoshilet.cli import Client, pack, parser


class CLITests(unittest.TestCase):
    def test_package_tree_and_reject_links(self):
        with tempfile.TemporaryDirectory() as root:
            source = Path(root) / 'source'
            source.mkdir()
            (source / 'index.html').write_text('hello')
            target = Path(root) / 'site.zip'
            pack(source, target)
            with zipfile.ZipFile(target) as z:
                self.assertEqual(z.read('index.html'), b'hello')
            (source / 'link').symlink_to('/etc/passwd')
            with self.assertRaises(ValueError):
                pack(source, target)

    def test_origin_and_commands(self):
        for url in [
            'http://remote.example',
            'https://user:pass@example.com',
            'https://example.com/api',
        ]:
            with self.assertRaises(ValueError):
                Client(url)
        Client('https://example.com')
        Client('http://127.0.0.1:1234')
        args = parser().parse_args(
            ['--json', 'deploy', 'dist', '--oidc', '--project-id', '123', '--no-activate']
        )
        self.assertTrue(args.oidc and args.no_activate and args.json)


if __name__ == '__main__':
    unittest.main()
