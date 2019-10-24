#pragma once

#include "src/stirling/http2.h"
#include "src/stirling/http_parse.h"
#include "src/stirling/mysql_parse.h"

namespace pl {
namespace stirling {

/**
 * A map from an EntryType to MessageType.
 * Example usage:
 *   GetMessageType<mysql::Entry>::type --> mysql::Packet
 *
 * @tparam TEntryType The higher-level entry type, which is they map 'key'.
 */

template <class TEntryType>
struct GetMessageType;

template <>
struct GetMessageType<http::Record> {
  typedef http::HTTPMessage type;
};

template <>
struct GetMessageType<http2::Record> {
  typedef http2::Frame type;
};

template <>
struct GetMessageType<mysql::Record> {
  typedef mysql::Packet type;
};

inline std::string_view ProtocolName(TrafficProtocol protocol) {
  switch (protocol) {
    case kProtocolUnknown:
      return "Unknown";
    case kProtocolHTTP:
      return "HTTP";
    case kProtocolHTTP2:
      return "HTTP2";
    case kProtocolMySQL:
      return "MySQL";
    default:
      return "unknown";
  }
}

}  // namespace stirling
}  // namespace pl
