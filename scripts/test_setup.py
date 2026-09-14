import os
from pathlib import Path
import subprocess
import sys
import tempfile
import tomllib
import unittest

SCRIPT=Path(__file__).with_name("configure_codex.py")

class SetupTest(unittest.TestCase):
    def test_preserves_other_settings_credentials_and_protected_rollback(self):
        with tempfile.TemporaryDirectory() as tmp:
            config=Path(tmp)/"config.toml"
            original='''model = "unchanged"
[mcp_servers.mcp-trello]
command = "/old/command"
args = ["old"]
enabled = true
tool_timeout_sec = 45
[mcp_servers.mcp-trello.env]
TRELLO_API_KEY = "fixture-key"
TRELLO_TOKEN = "fixture-token"
TRELLO_ALLOWED_BOARD_IDS = "fixture-board"
[mcp_servers.other]
command = "unchanged"
'''
            config.write_text(original)
            result=subprocess.run([sys.executable,str(SCRIPT),"--config",str(config)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            updated=tomllib.loads(config.read_text())
            before=tomllib.loads(original)
            self.assertEqual(updated["mcp_servers"]["other"],before["mcp_servers"]["other"])
            new=updated["mcp_servers"]["mcp-trello"]
            self.assertEqual(new["env"],before["mcp_servers"]["mcp-trello"]["env"])
            self.assertEqual(new["tool_timeout_sec"],45)
            self.assertTrue(new["command"].endswith("/dist/trello-mcp"))
            backups=list(Path(tmp).glob("*.bak"))
            self.assertEqual(len(backups),1)
            self.assertEqual(backups[0].read_text(),original)
            self.assertEqual(os.stat(backups[0]).st_mode & 0o777,0o600)
            self.assertNotIn("fixture-token",result.stdout+result.stderr)
            rerun=subprocess.run([sys.executable,str(SCRIPT),"--config",str(config)],capture_output=True,text=True)
            self.assertEqual(rerun.returncode,0,rerun.stderr)
            self.assertIn("Already configured",rerun.stdout)

if __name__=="__main__":unittest.main()
