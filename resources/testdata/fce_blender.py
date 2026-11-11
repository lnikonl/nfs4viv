"""Exercise Blender's actual OBJ importer/exporter, including a topology edit."""
import os
import sys

import bpy

directory = sys.argv[1]
bpy.ops.wm.read_factory_settings(use_empty=True)
bpy.ops.wm.obj_import(
    filepath=os.path.join(directory, "model.obj"),
    forward_axis="NEGATIVE_Z",
    up_axis="Y",
)
objects = sorted(bpy.context.scene.objects, key=lambda obj: obj.name)
assert len(objects) >= 2, [obj.name for obj in objects]
first = objects[0]
bpy.context.view_layer.objects.active = first
bpy.ops.object.select_all(action="DESELECT")
first.select_set(True)
first.location.x += 2.0
bpy.ops.object.mode_set(mode="EDIT")
bpy.ops.mesh.select_all(action="SELECT")
bpy.ops.mesh.subdivide(number_cuts=1)
bpy.ops.object.mode_set(mode="OBJECT")
bpy.ops.object.select_all(action="SELECT")
bpy.ops.wm.obj_export(
    filepath=os.path.join(directory, "model.obj"),
    forward_axis="NEGATIVE_Z",
    up_axis="Y",
    export_selected_objects=True,
    export_uv=True,
    export_normals=True,
    export_materials=True,
    export_triangulated_mesh=True,
)
print("Blender", bpy.app.version_string, "parts:", [obj.name for obj in objects])
