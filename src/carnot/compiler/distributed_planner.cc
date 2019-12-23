#include <memory>
#include <string>
#include <unordered_set>
#include <vector>

#include "src/carnot/compiler/ast_visitor.h"
#include "src/carnot/compiler/distributed_coordinator.h"
#include "src/carnot/compiler/distributed_planner.h"
#include "src/carnot/compiler/distributed_stitcher.h"
#include "src/carnot/compiler/rules.h"
namespace pl {
namespace carnot {
namespace compiler {
namespace distributed {

StatusOr<std::unique_ptr<DistributedPlanner>> DistributedPlanner::Create() {
  std::unique_ptr<DistributedPlanner> planner(new DistributedPlanner());
  PL_RETURN_IF_ERROR(planner->Init());
  return planner;
}

Status DistributedPlanner::Init() { return Status::OK(); }

StatusOr<std::unique_ptr<DistributedPlan>> DistributedPlanner::Plan(
    const distributedpb::DistributedState& distributed_state, CompilerState* compiler_state,
    const IR* logical_plan) {
  PL_ASSIGN_OR_RETURN(std::unique_ptr<Coordinator> coordinator,
                      Coordinator::Create(distributed_state));
  PL_ASSIGN_OR_RETURN(std::unique_ptr<Stitcher> stitcher, Stitcher::Create(compiler_state));

  PL_ASSIGN_OR_RETURN(std::unique_ptr<DistributedPlan> distributed_plan,
                      coordinator->Coordinate(logical_plan));
  PL_RETURN_IF_ERROR(stitcher->Stitch(distributed_plan.get()));

  return distributed_plan;
}

StatusOr<std::unique_ptr<NoKelvinPlanner>> NoKelvinPlanner::Create() {
  std::unique_ptr<NoKelvinPlanner> planner(new NoKelvinPlanner());
  return planner;
}

StatusOr<std::unique_ptr<DistributedPlan>> NoKelvinPlanner::Plan(
    const distributedpb::DistributedState& distributed_state, CompilerState*,
    const IR* logical_plan) {
  PL_ASSIGN_OR_RETURN(std::unique_ptr<NoRemoteCoordinator> coordinator,
                      NoRemoteCoordinator::Create(distributed_state));
  PL_ASSIGN_OR_RETURN(std::unique_ptr<DistributedPlan> distributed_plan,
                      coordinator->Coordinate(logical_plan));

  return distributed_plan;
}

}  // namespace distributed
}  // namespace compiler
}  // namespace carnot
}  // namespace pl
