import os
import unittest
from unittest.mock import patch
import whatsapp

class ApiBaseUrlTests(unittest.TestCase):
    def resolve(self, **env):
        with patch.dict(os.environ, env, clear=True):
            return whatsapp._resolve_api_base_url()

    def test_default_is_localhost(self):
        self.assertEqual(self.resolve(), "http://localhost:8080/api")

    def test_follows_bridge_host_and_port(self):
        self.assertEqual(self.resolve(WHATSAPP_API_HOST="100.64.0.1", WHATSAPP_API_PORT="8081"), "http://100.64.0.1:8081/api")

    def test_wildcard_bind_uses_localhost(self):
        self.assertEqual(self.resolve(WHATSAPP_API_HOST="0.0.0.0"), "http://localhost:8080/api")
        self.assertEqual(self.resolve(WHATSAPP_API_HOST="::"), "http://localhost:8080/api")

    def test_ipv6_host_is_bracketed(self):
        self.assertEqual(self.resolve(WHATSAPP_API_HOST="::1"), "http://[::1]:8080/api")

    def test_base_url_overrides_host(self):
        self.assertEqual(self.resolve(WHATSAPP_API_HOST="100.64.0.1", WHATSAPP_API_BASE_URL="http://bridge:9000/api/"), "http://bridge:9000/api")

if __name__ == "__main__":
    unittest.main()
