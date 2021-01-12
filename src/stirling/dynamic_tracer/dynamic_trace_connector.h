#pragma once

#include <deque>
#include <memory>
#include <string>
#include <utility>

#include "src/common/base/base.h"
#include "src/stirling/bpf_tools/bcc_wrapper.h"
#include "src/stirling/core/source_connector.h"
#include "src/stirling/dynamic_tracer/dynamic_tracing/dynamic_tracer.h"

namespace pl {
namespace stirling {

class DynamicTraceConnector : public SourceConnector, public bpf_tools::BCCWrapper {
 public:
  ~DynamicTraceConnector() override = default;

  static StatusOr<std::unique_ptr<SourceConnector>> Create(
      std::string_view name, dynamic_tracing::ir::logical::TracepointDeployment* program);

  // Accepts a piece of data from the perf buffer.
  void AcceptDataEvents(std::string data) { data_items_.push_back(std::move(data)); }

 protected:
  // TODO(oazizi): This constructor only works with a single table,
  //               since the ArrayView creation only works for a single schema.
  //               Consider how to expand to multiple tables if/when needed.
  DynamicTraceConnector(std::string_view name, std::unique_ptr<DynamicDataTableSchema> table_schema,
                        dynamic_tracing::BCCProgram bcc_program)
      : SourceConnector(name, ArrayView<DataTableSchema>(&table_schema->Get(), 1)),
        table_schema_(std::move(table_schema)),
        bcc_program_(std::move(bcc_program)) {}

  Status InitImpl() override;

  void TransferDataImpl(ConnectorContext* ctx, uint32_t table_num, DataTable* data_table) override;

  Status StopImpl() override { return Status::OK(); }

 private:
  Status AppendRecord(const ::pl::stirling::dynamic_tracing::ir::physical::Struct& st,
                      uint32_t asid, std::string_view buf, DataTable* data_table);

  // Describes the output table column types.
  std::unique_ptr<DynamicDataTableSchema> table_schema_;

  // The actual dynamic trace program.
  dynamic_tracing::BCCProgram bcc_program_;

  // A buffer to hold raw data items from the perf buffer.
  std::deque<std::string> data_items_;
};

// Converts proto specification of columns into the form that is used by TableSchema.
// Only public for testing purposes.
BackedDataElements ConvertFields(
    const google::protobuf::RepeatedPtrField<dynamic_tracing::ir::physical::Field>&
        repeated_fields);

}  // namespace stirling
}  // namespace pl