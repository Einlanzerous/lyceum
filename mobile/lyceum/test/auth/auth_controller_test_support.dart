import 'package:lyceum/api/server_store.dart';
import 'package:lyceum/auth/session_store.dart';

/// An in-memory keystore.
class FakeTokenStore implements TokenStore {
  FakeTokenStore([this.token = '']);
  String token;

  @override
  Future<String> read() async => token;

  @override
  Future<void> write(String value) async => token = value;

  @override
  Future<void> delete() async => token = '';
}

/// Pins the server URL so the client builds real request URIs under test.
class FixedServerUrl extends ServerUrlController {
  @override
  String build() => 'http://lib.test';
}
