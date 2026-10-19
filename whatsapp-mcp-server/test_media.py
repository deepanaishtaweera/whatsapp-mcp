import unittest
from unittest.mock import Mock, patch
import requests
import whatsapp

class MediaDownloadTests(unittest.TestCase):
    def test_signed_download_success(self):
        response = Mock(status_code=200)
        response.json.return_value = {"success": True, "path": "/tmp/program.pdf"}
        with patch.object(whatsapp.requests, "post", return_value=response) as post:
            result = whatsapp.download_media("message", "chat")
        self.assertEqual(result["file_path"], "/tmp/program.pdf")
        self.assertEqual(post.call_args.kwargs["timeout"], 60)

    def test_bridge_failure_remains_visible(self):
        response = Mock(status_code=200)
        response.json.return_value = {"success": False, "message": "Download failed with status code 403"}
        with patch.object(whatsapp.requests, "post", return_value=response):
            result = whatsapp.download_media("message", "chat")
        self.assertFalse(result["success"])
        self.assertIn("403", result["message"])

    def test_missing_path_is_not_success(self):
        response = Mock(status_code=200)
        response.json.return_value = {"success": True}
        with patch.object(whatsapp.requests, "post", return_value=response):
            self.assertFalse(whatsapp.download_media("message", "chat")["success"])

    def test_timeout_does_not_expose_request_details(self):
        with patch.object(whatsapp.requests, "post", side_effect=requests.Timeout("private url")):
            self.assertEqual(whatsapp.download_media("message", "chat")["message"], "Media download timed out")

if __name__ == '__main__':
    unittest.main()
